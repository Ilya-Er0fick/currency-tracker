package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

var ErrAssetNotFound = errors.New("asset not found")

type Asset struct {
	ID                int    `json:"-"`
	Symbol            string `json:"symbol"`
	Type              string `json:"type"`
	Source            string `json:"source"`
	ResolutionSeconds int    `json:"resolution_seconds"`
	SupportsRealtime  bool   `json:"supports_realtime"`
}

type Assets struct {
	pool *pgxpool.Pool
}

func NewAssets(pool *pgxpool.Pool) *Assets {
	return &Assets{pool: pool}
}

func (a *Assets) List(ctx context.Context) ([]Asset, error) {
	rows, err := a.pool.Query(ctx,
		`SELECT id, symbol, type, source, resolution_seconds, supports_realtime
		 FROM assets ORDER BY type, symbol`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	assets := make([]Asset, 0, 64)
	for rows.Next() {
		var asset Asset
		if err := rows.Scan(&asset.ID, &asset.Symbol, &asset.Type, &asset.Source,
			&asset.ResolutionSeconds, &asset.SupportsRealtime); err != nil {
			return nil, err
		}
		assets = append(assets, asset)
	}
	return assets, rows.Err()
}

func (a *Assets) BySymbol(ctx context.Context, symbol string) (Asset, error) {
	var asset Asset
	err := a.pool.QueryRow(ctx,
		`SELECT id, symbol, type, source, resolution_seconds, supports_realtime
		 FROM assets WHERE symbol = $1`, symbol).
		Scan(&asset.ID, &asset.Symbol, &asset.Type, &asset.Source,
			&asset.ResolutionSeconds, &asset.SupportsRealtime)

	if errors.Is(err, pgx.ErrNoRows) {
		return Asset{}, ErrAssetNotFound
	}
	if err != nil {
		return Asset{}, err
	}
	return asset, nil
}
