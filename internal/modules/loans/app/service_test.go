package app_test

import (
	"context"
	"errors"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
	"time"
)

type clock struct{}

func (clock) Now() time.Time { return time.Date(2026, 10, 24, 17, 0, 0, 0, time.UTC) }

type profile struct{}

func (profile) Today(context.Context, uuid.UUID) (dates.LocalDate, error) {
	return dates.New(2026, 10, 24), nil
}

type transaction struct{}

func (transaction) InTx(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }

type repository struct {
	app.Repository
	debtor                    domain.Debtor
	locks, saves, sharedCalls int
	lockErr                   error
}

func (r *repository) Get(context.Context, uuid.UUID, uuid.UUID) (domain.Debtor, error) {
	return r.debtor, nil
}
func (r *repository) Lock(context.Context, uuid.UUID, uuid.UUID) error { r.locks++; return r.lockErr }
func (r *repository) Insert(_ context.Context, _ uuid.UUID, d domain.Debtor) (bool, error) {
	r.debtor = d
	return true, nil
}
func (r *repository) InsertMovement(context.Context, uuid.UUID, uuid.UUID, domain.Movement) (bool, error) {
	r.saves++
	return true, nil
}
func (r *repository) SavePlans(context.Context, uuid.UUID, uuid.UUID, []domain.Plan) error {
	r.saves++
	return nil
}
func (r *repository) Shared(context.Context, string) (domain.Debtor, error) {
	r.sharedCalls++
	return r.debtor, nil
}
func service(r *repository) *app.Service {
	return app.New(app.Deps{Repo: r, Tx: transaction{}, Clock: clock{}, Profile: profile{}})
}
func amount(value string) money.Amount {
	a, err := money.ParseAmount(value)
	if err != nil {
		panic(err)
	}
	return a
}
func TestCreateAlwaysStartsPrivateAndNormalizesName(t *testing.T) {
	repo := &repository{}
	unsafe := "client-token"
	d, created, err := service(repo).Create(context.Background(), uuid.New(), domain.Debtor{Name: " Gerson ", Currency: "PEN", ShareToken: &unsafe})
	require.NoError(t, err)
	require.True(t, created)
	require.Equal(t, "Gerson", d.Name)
	require.Nil(t, d.ShareToken)
	require.NotEqual(t, uuid.Nil, d.ID)
}
func TestInvalidBalanceNeverPersistsAndOwnerLockPrecedesMutation(t *testing.T) {
	repo := &repository{debtor: domain.Debtor{ID: uuid.New(), Name: "Gerson", Currency: "PEN"}}
	_, _, err := service(repo).SaveMovement(context.Background(), uuid.New(), repo.debtor.ID, domain.Movement{Kind: "payment", Amount: amount("100"), Date: dates.New(2026, 10, 1), AppliedMonth: "2026-10"}, false)
	require.ErrorIs(t, err, domain.ErrBalance)
	require.Equal(t, 1, repo.locks)
	require.Zero(t, repo.saves)
	repo.lockErr = domain.ErrNotFound
	_, err = service(repo).Plan(context.Background(), uuid.New(), repo.debtor.ID, nil)
	require.True(t, errors.Is(err, domain.ErrNotFound))
	require.Zero(t, repo.saves)
}
func TestInvalidShareTokensDoNotReachTheRepository(t *testing.T) {
	repo := &repository{}
	for _, token := range []string{"", "short", "../../../x"} {
		_, err := service(repo).Shared(context.Background(), token)
		require.ErrorIs(t, err, domain.ErrNotFound)
	}
	require.Zero(t, repo.sharedCalls)
}
