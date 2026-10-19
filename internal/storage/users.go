package storage

import (
	"context"
	"errors"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
)

var (
	ErrEmailTaken   = errors.New("email is already registered")
	ErrUserNotFound = errors.New("user not found")
)

// uniqueViolation — код ошибки PostgreSQL при нарушении уникального ключа.
const uniqueViolation = "23505"

type User struct {
	ID           int64
	Email        string
	PasswordHash string
}

type Users struct {
	pool *pgxpool.Pool
}

func NewUsers(pool *pgxpool.Pool) *Users {
	return &Users{pool: pool}
}

// Create полагается на уникальный индекс по адресу, а не на предварительную
// проверку: иначе между проверкой и вставкой мог бы вклиниться другой запрос.
func (u *Users) Create(ctx context.Context, email, passwordHash string) (int64, error) {
	var id int64
	err := u.pool.QueryRow(ctx,
		`INSERT INTO users (email, password_hash) VALUES ($1, $2) RETURNING id`,
		email, passwordHash).Scan(&id)

	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return 0, ErrEmailTaken
	}
	if err != nil {
		return 0, err
	}
	return id, nil
}

func (u *Users) ByEmail(ctx context.Context, email string) (User, error) {
	var user User
	err := u.pool.QueryRow(ctx,
		`SELECT id, email, password_hash FROM users WHERE email = $1`,
		email).Scan(&user.ID, &user.Email, &user.PasswordHash)

	if errors.Is(err, pgx.ErrNoRows) {
		return User{}, ErrUserNotFound
	}
	if err != nil {
		return User{}, err
	}
	return user, nil
}
