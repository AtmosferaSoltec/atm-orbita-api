// Package testsupport tiene ayudantes para las pruebas de integración.
package testsupport

import (
	"context"
	"os"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/database"
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
