// Package testsupport tiene ayudantes para las pruebas de integración.
package testsupport

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"sync"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/db"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// DatabaseURLVar apunta al Postgres local (docker-compose.dev.yml). El check del
// despliegue no tiene base: sin esta variable, las pruebas de integración se saltan.
const DatabaseURLVar = "TEST_DATABASE_URL"

// DatabaseURL devuelve la URL de la base de pruebas, o salta la prueba.
func DatabaseURL(t *testing.T) string {
	t.Helper()
	url := os.Getenv(DatabaseURLVar)
	if url == "" {
		t.Skipf("%s is not set: integration test skipped", DatabaseURLVar)
	}
	return url
}

// Pool abre un pool contra la base de pruebas y lo cierra al terminar.
func Pool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	pool, err := database.Open(context.Background(), DatabaseURL(t))
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	return pool
}

var migrateOnce sync.Once
var migrateErr error

// MigratedPool aplica las migraciones pendientes (una vez por paquete de
// pruebas) y abre un pool. Las pruebas de los módulos parten de aquí.
func MigratedPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := DatabaseURL(t)
	migrateOnce.Do(func() {
		quiet := slog.New(slog.NewTextHandler(io.Discard, nil))
		migrateErr = database.Migrate(context.Background(), url, db.Migrations, database.MigrateUp, quiet)
	})
	require.NoError(t, migrateErr)
	return Pool(t)
}

// UniqueEmail da un correo que no choca con otros datos de la base local, bajo
// un dominio reservado para pruebas.
func UniqueEmail(label string) string {
	return fmt.Sprintf("%s-%s@e2e.orbita.test", label, ids.New())
}

// DeleteUsersByEmail borra al terminar las cuentas de prueba; la cascada se lleva
// todo lo demás.
func DeleteUsersByEmail(t *testing.T, pool *pgxpool.Pool, emails ...string) {
	t.Helper()
	t.Cleanup(func() {
		_, err := pool.Exec(context.Background(), `DELETE FROM users WHERE email = ANY($1)`, emails)
		if err != nil {
			t.Errorf("cleanup users: %v", err)
		}
	})
}
