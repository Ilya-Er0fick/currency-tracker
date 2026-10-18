// Пакет storage отвечает за подключение к базе и применение миграций.
package storage

import (
	"context"
	"embed"
	"fmt"
	"sort"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

// Migrate применяет недостающие миграции по возрастанию имени файла.
// Каждая выполняется в своей транзакции вместе с отметкой о применении,
// поэтому прерванный запуск не оставляет базу в промежуточном состоянии,
// а повторный запуск ничего не делает.
func Migrate(ctx context.Context, pool *pgxpool.Pool) ([]string, error) {
	if _, err := pool.Exec(ctx, `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			version    text PRIMARY KEY,
			applied_at timestamptz NOT NULL DEFAULT now()
		)`); err != nil {
		return nil, fmt.Errorf("create schema_migrations: %w", err)
	}

	applied, err := appliedVersions(ctx, pool)
	if err != nil {
		return nil, err
	}

	entries, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return nil, err
	}

	versions := make([]string, 0, len(entries))
	for _, entry := range entries {
		versions = append(versions, entry.Name())
	}
	sort.Strings(versions)

	var executed []string
	for _, version := range versions {
		if applied[version] {
			continue
		}
		if err := applyOne(ctx, pool, version); err != nil {
			return executed, fmt.Errorf("migration %s: %w", version, err)
		}
		executed = append(executed, version)
	}

	return executed, nil
}

func appliedVersions(ctx context.Context, pool *pgxpool.Pool) (map[string]bool, error) {
	rows, err := pool.Query(ctx, `SELECT version FROM schema_migrations`)
	if err != nil {
		return nil, fmt.Errorf("read schema_migrations: %w", err)
	}
	defer rows.Close()

	applied := make(map[string]bool)
	for rows.Next() {
		var version string
		if err := rows.Scan(&version); err != nil {
			return nil, err
		}
		applied[version] = true
	}

	return applied, rows.Err()
}

func applyOne(ctx context.Context, pool *pgxpool.Pool, version string) error {
	body, err := migrationFiles.ReadFile("migrations/" + version)
	if err != nil {
		return err
	}

	return pgx.BeginFunc(ctx, pool, func(tx pgx.Tx) error {
		if _, err := tx.Exec(ctx, string(body)); err != nil {
			return err
		}
		_, err := tx.Exec(ctx, `INSERT INTO schema_migrations (version) VALUES ($1)`, version)
		return err
	})
}
