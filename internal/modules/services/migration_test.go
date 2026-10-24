package services_test

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

// The migration is rehearsed in an isolated, rolled-back schema on the same
// server. No existing Orbita table or migration version is changed by this test.
func TestMigrationUpAndDownPreserveExistingUser(t *testing.T) {
	pool := testsupport.Pool(t)
	ctx := context.Background()
	tx, err := pool.Begin(ctx)
	require.NoError(t, err)
	defer func() { _ = tx.Rollback(ctx) }()
	schema := "services_migration_qa"
	identifier := pgx.Identifier{schema}.Sanitize()
	_, err = tx.Exec(ctx, "CREATE SCHEMA "+identifier)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, "SET LOCAL search_path TO "+identifier)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `CREATE TABLE users(id UUID PRIMARY KEY); INSERT INTO users VALUES ('01950000-0000-7000-8000-000000000001')`)
	require.NoError(t, err)
	raw, err := os.ReadFile("../../../db/migrations/00014_services_create_subscriptions.sql")
	require.NoError(t, err)
	up, down, ok := strings.Cut(string(raw), "-- +goose Down")
	require.True(t, ok)
	up = strings.Replace(up, "-- +goose Up", "", 1)
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, `INSERT INTO services(id,user_id,name,color,currency,amount,billing_day,started_on,status) VALUES(gen_random_uuid(),'01950000-0000-7000-8000-000000000001','Migration probe','#1D4ED8','PEN',10,31,'2026-01-01','active')`)
	require.NoError(t, err)
	_, err = tx.Exec(ctx, down)
	require.NoError(t, err)
	var count int
	require.NoError(t, tx.QueryRow(ctx, "SELECT count(*) FROM users").Scan(&count))
	require.Equal(t, 1, count)
	_, err = tx.Exec(ctx, up)
	require.NoError(t, err)
}
