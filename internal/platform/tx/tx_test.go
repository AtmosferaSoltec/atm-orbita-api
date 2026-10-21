package tx_test

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Una tabla de prueba propia de este paquete, que se borra al terminar.
func setup(t *testing.T) (tx.Runner, context.Context) {
	t.Helper()
	pool := testsupport.Pool(t)
	ctx := context.Background()
	_, err := pool.Exec(ctx, `CREATE TABLE IF NOT EXISTS tx_probe (id INT PRIMARY KEY)`)
	require.NoError(t, err)
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DROP TABLE IF EXISTS tx_probe`) })
	return tx.NewRunner(pool), ctx
}

func count(t *testing.T, runner tx.Runner, ctx context.Context) int {
	t.Helper()
	var n int
	require.NoError(t, runner.DB(ctx).QueryRow(ctx, `SELECT count(*) FROM tx_probe`).Scan(&n))
	return n
}

func TestEverythingIsSavedTogether(t *testing.T) {
	runner, ctx := setup(t)
	err := runner.InTx(ctx, func(ctx context.Context) error {
		_, err := runner.DB(ctx).Exec(ctx, `INSERT INTO tx_probe VALUES (1), (2)`)
		return err
	})
	require.NoError(t, err)
	assert.Equal(t, 2, count(t, runner, ctx))
}

func TestAnErrorUndoesEverything(t *testing.T) {
	runner, ctx := setup(t)
	failure := errors.New("second step failed")
	err := runner.InTx(ctx, func(ctx context.Context) error {
		if _, err := runner.DB(ctx).Exec(ctx, `INSERT INTO tx_probe VALUES (1)`); err != nil {
			return err
		}
		return failure
	})
	assert.ErrorIs(t, err, failure)
	assert.Equal(t, 0, count(t, runner, ctx))
}

func TestANestedCallJoinsTheSameTransaction(t *testing.T) {
	runner, ctx := setup(t)
	failure := errors.New("outer step failed")
	err := runner.InTx(ctx, func(ctx context.Context) error {
		err := runner.InTx(ctx, func(ctx context.Context) error {
			_, err := runner.DB(ctx).Exec(ctx, `INSERT INTO tx_probe VALUES (1)`)
			return err
		})
		if err != nil {
			return err
		}
		return failure
	})
	assert.ErrorIs(t, err, failure)
	// El paso interno no quedó guardado por su cuenta.
	assert.Equal(t, 0, count(t, runner, ctx))
}

func TestAPanicUndoesEverything(t *testing.T) {
	runner, ctx := setup(t)
	assert.Panics(t, func() {
		_ = runner.InTx(ctx, func(ctx context.Context) error {
			_, _ = runner.DB(ctx).Exec(ctx, `INSERT INTO tx_probe VALUES (1)`)
			panic("boom")
		})
	})
	assert.Equal(t, 0, count(t, runner, ctx))
}
