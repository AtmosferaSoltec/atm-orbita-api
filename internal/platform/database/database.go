// Package database abre el pool de conexiones a PostgreSQL.
package database

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

// statementTimeout: una consulta desbocada no puede retener el pool. Va como
// parámetro de la sesión, así lo aplica el propio Postgres.
const statementTimeout = "5s"

// Open crea el pool. El search_path (el esquema orbita) viaja en la URL
// (?search_path=orbita) y además está fijado en el rol por provision-db.sql.
func Open(ctx context.Context, databaseURL string) (*pgxpool.Pool, error) {
	cfg, err := pgxpool.ParseConfig(databaseURL)
	if err != nil {
		// No se envuelve err: su texto podría incluir la URL con la contraseña.
		return nil, fmt.Errorf("parse database url: invalid format")
	}
	cfg.MaxConns = 10
	cfg.MaxConnIdleTime = 5 * time.Minute
	cfg.ConnConfig.ConnectTimeout = 5 * time.Second
	if cfg.ConnConfig.RuntimeParams == nil {
		cfg.ConnConfig.RuntimeParams = map[string]string{}
	}
	cfg.ConnConfig.RuntimeParams["statement_timeout"] = statementTimeout
	cfg.ConnConfig.RuntimeParams["application_name"] = "orbita-api"

	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	if err != nil {
		return nil, fmt.Errorf("create database pool: %w", err)
	}
	return pool, nil
}

// Ping comprueba que la base responde, con un tope de tiempo propio.
func Ping(ctx context.Context, pool *pgxpool.Pool, timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	return pool.Ping(ctx)
}
