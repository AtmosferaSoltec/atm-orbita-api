package centralauth_test

import (
	"context"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/centralauth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

func TestProvisionRunsSetupOnceAndDeleteCascades(t *testing.T) {
	pool := testsupport.MigratedPool(t)
	runner := tx.NewRunner(pool)
	ctx := context.Background()

	setups := 0
	deleted := 0
	people := centralauth.Users{
		Tx: runner, MainCurrency: "PEN", Timezone: "America/Lima",
		Setup: func(ctx context.Context, userID uuid.UUID, currency, timezone string) error {
			setups++
			_, err := runner.DB(ctx).Exec(ctx, `INSERT INTO profiles (user_id, main_currency, secondary_currency, display_currency, timezone) VALUES ($1, $2, 'USD', $2, $3)`, userID, currency, timezone)
			return err
		},
		OnDelete: []centralauth.DeletionHook{func(context.Context, uuid.UUID) error { deleted++; return nil }},
	}
	id := uuid.New()
	email := testsupport.UniqueEmail("central")
	t.Cleanup(func() { _, _ = pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id) })

	require.NoError(t, people.Provision(ctx, id, email, "Ana"))
	require.NoError(t, people.Provision(ctx, id, email, "Ana"))
	assert.Equal(t, 1, setups, "an existing person is not set up again")

	require.NoError(t, people.Delete(ctx, id))
	require.NoError(t, people.Delete(ctx, id), "deleting twice is harmless")
	assert.Equal(t, 2, deleted)
	var left int
	require.NoError(t, pool.QueryRow(ctx, `SELECT (SELECT count(*) FROM users WHERE id=$1) + (SELECT count(*) FROM profiles WHERE user_id=$1)`, id).Scan(&left))
	assert.Zero(t, left, "the profile goes with the user")
}
