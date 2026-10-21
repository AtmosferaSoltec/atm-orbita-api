package database_test

import (
	"context"
	"io"
	"log/slog"
	"testing"
	"testing/fstest"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/db"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

func quiet() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func TestMigrateKeepsItsControlTableInTheOrbitaSchema(t *testing.T) {
	url := testsupport.DatabaseURL(t)
	ctx := context.Background()

	require.NoError(t, database.Migrate(ctx, url, db.Migrations, database.MigrateUp, quiet()))

	pool := testsupport.Pool(t)
	var schema string
	err := pool.QueryRow(ctx,
		`SELECT table_schema FROM information_schema.tables WHERE table_name = 'goose_db_version'`,
	).Scan(&schema)
	require.NoError(t, err)
	assert.Equal(t, "orbita", schema)
}

func TestMigrateUpThenDown(t *testing.T) {
	url := testsupport.DatabaseURL(t)
	ctx := context.Background()
	pool := testsupport.Pool(t)

	// Una migración de prueba con un número que nunca usará una real.
	migrations := fstest.MapFS{
		"migrations/99999000000001_platform_probe.sql": {Data: []byte(`
-- +goose Up
CREATE TABLE platform_probe (id INT PRIMARY KEY);
-- +goose Down
DROP TABLE platform_probe;
`)},
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS platform_probe`)
		_, _ = pool.Exec(ctx, `DELETE FROM goose_db_version WHERE version_id = 99999000000001`)
	})

	require.NoError(t, database.Migrate(ctx, url, migrations, database.MigrateUp, quiet()))
	assert.True(t, tableExists(t, pool, "platform_probe"))

	require.NoError(t, database.Migrate(ctx, url, migrations, database.MigrateDown, quiet()))
	assert.False(t, tableExists(t, pool, "platform_probe"))
}

func TestAnUnknownMigrateCommandFails(t *testing.T) {
	url := testsupport.DatabaseURL(t)
	err := database.Migrate(context.Background(), url, db.Migrations, "sideways", quiet())
	assert.ErrorContains(t, err, "unknown migrate command")
}

func tableExists(t *testing.T, pool *pgxpool.Pool, name string) bool {
	t.Helper()
	var exists bool
	err := pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM information_schema.tables WHERE table_schema = 'orbita' AND table_name = $1)`,
		name,
	).Scan(&exists)
	require.NoError(t, err)
	return exists
}
