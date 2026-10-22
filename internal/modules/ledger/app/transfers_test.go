package app

import (
	"context"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

type fakeTransfers struct {
	rows  map[uuid.UUID]StoredTransfer
	users map[uuid.UUID]uuid.UUID
}

func (f *fakeTransfers) Insert(_ context.Context, userID uuid.UUID, t domain.Transfer, _ time.Time) (bool, error) {
	if _, ok := f.rows[t.ID]; ok {
		return false, nil
	}
	f.rows[t.ID] = StoredTransfer{Transfer: t}
	f.users[t.ID] = userID
	return true, nil
}

func (f *fakeTransfers) Get(_ context.Context, userID, id uuid.UUID) (StoredTransfer, error) {
	row, ok := f.rows[id]
	if !ok || f.users[id] != userID {
		return StoredTransfer{}, domain.ErrNotFound
	}
	return row, nil
}

func (f *fakeTransfers) Update(_ context.Context, _ uuid.UUID, t domain.Transfer) error {
	f.rows[t.ID] = StoredTransfer{Transfer: t}
	return nil
}

func (f *fakeTransfers) Delete(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	row := f.rows[id]
	row.Deleted = true
	f.rows[id] = row
	return nil
}

type transferFixture struct {
	service             *TransferService
	user                uuid.UUID
	usd, debit, savings uuid.UUID
	old                 uuid.UUID
}

func newTransferFixture(t *testing.T) transferFixture {
	t.Helper()
	ctx := context.Background()
	accounts := &fakeAccounts{}
	f := transferFixture{user: uuid.New(), usd: uuid.New(), debit: uuid.New(), savings: uuid.New(), old: uuid.New()}
	for id, currency := range map[uuid.UUID]string{f.usd: "USD", f.debit: "PEN", f.savings: "PEN", f.old: "PEN"} {
		_, err := accounts.Insert(ctx, f.user, NewAccount{ID: id, Name: currency, Type: domain.AccountSavings, Currency: currency}, time.Time{})
		require.NoError(t, err)
	}
	require.NoError(t, accounts.Archive(ctx, f.user, f.old))
	profile := &fakeFx{today: dates.New(2026, time.October, 2)}
	f.service = NewTransferService(&fakeTransfers{rows: map[uuid.UUID]StoredTransfer{}, users: map[uuid.UUID]uuid.UUID{}},
		accounts, profile, clock.Fixed{At: time.Date(2026, 10, 2, 12, 0, 0, 0, time.UTC)})
	return f
}

func TestTransferValidationFollowsTheAppOrder(t *testing.T) {
	f := newTransferFixture(t)
	_, _, err := f.service.Create(context.Background(), f.user, nil, TransferInput{
		FromAccountID: &f.debit, ToAccountID: &f.debit, FromAmount: ptr("0"), Note: ptr(string(make([]byte, 501))),
		OccurredOn: ptr("2026-10-03"),
	})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{
		{Field: "to_account_id", Code: domain.CodeSameAccount},
		{Field: "from_amount", Code: domain.CodeAmountRequired},
		{Field: "to_amount", Code: domain.CodeAmountRequired},
		{Field: "note", Code: domain.CodeDescriptionTooLong},
		{Field: "occurred_on", Code: domain.CodeFutureDate},
	}, problems.Fields)
}

func TestATransferBetweenCurrenciesKeepsWhatTheAppSent(t *testing.T) {
	f := newTransferFixture(t)
	entry, created, err := f.service.Create(context.Background(), f.user, nil, TransferInput{
		FromAccountID: &f.usd, ToAccountID: &f.debit, FromAmount: ptr("20.00"), ToAmount: ptr("63.40"),
		ExchangeRate: OptionalText{Set: true, Value: ptr("3.17")}, Note: ptr(" Cambio "),
	})
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "63.40", entry.ToAmount.StringFixed(2))
	assert.True(t, entry.ExchangeRate.Equal(decimal.RequireFromString("3.17")))
	assert.Equal(t, "Cambio", entry.Note)
	assert.Equal(t, "2026-10-02", entry.OccurredOn.String())
	assert.Equal(t, "USD", entry.From.Currency)

	// Pasarla a dos cuentas en soles con el tipo de cambio en null: misma moneda.
	updated, err := f.service.Update(context.Background(), f.user, entry.ID, TransferInput{
		FromAccountID: &f.savings, FromAmount: ptr("63.40"), ExchangeRate: OptionalText{Set: true},
	})
	require.NoError(t, err)
	assert.Nil(t, updated.ExchangeRate)
	assert.Equal(t, "PEN", updated.From.Currency)

	// Sin decir nada del tipo de cambio, se conserva el guardado (ninguno).
	_, err = f.service.Update(context.Background(), f.user, entry.ID, TransferInput{ToAmount: ptr("60.00")})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{{Field: "to_amount", Code: domain.CodeAmountsMustMatch}}, problems.Fields)
}

func TestTransfersRefuseArchivedAndForeignAccounts(t *testing.T) {
	f := newTransferFixture(t)
	_, _, err := f.service.Create(context.Background(), f.user, nil, TransferInput{
		FromAccountID: &f.old, ToAccountID: &f.debit, FromAmount: ptr("1.00"), ToAmount: ptr("1.00"),
	})
	assert.ErrorIs(t, err, domain.ErrAccountArchived)
	other := uuid.New()
	_, _, err = f.service.Create(context.Background(), f.user, nil, TransferInput{
		FromAccountID: &f.debit, ToAccountID: &other, FromAmount: ptr("1.00"), ToAmount: ptr("1.00"),
	})
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestTransferRetryAndDelete(t *testing.T) {
	f := newTransferFixture(t)
	id := uuid.New()
	in := TransferInput{FromAccountID: &f.debit, ToAccountID: &f.savings, FromAmount: ptr("500.00"), ToAmount: ptr("500.00")}
	_, created, err := f.service.Create(context.Background(), f.user, &id, in)
	require.NoError(t, err)
	assert.True(t, created)
	_, created, err = f.service.Create(context.Background(), f.user, &id, in)
	require.NoError(t, err)
	assert.False(t, created)
	in.Note = ptr("otra")
	_, _, err = f.service.Create(context.Background(), f.user, &id, in)
	assert.ErrorIs(t, err, domain.ErrIDTaken)

	require.NoError(t, f.service.Delete(context.Background(), f.user, id))
	require.NoError(t, f.service.Delete(context.Background(), f.user, id))
	_, err = f.service.Update(context.Background(), f.user, id, TransferInput{Note: ptr("x")})
	assert.ErrorIs(t, err, domain.ErrNotFound)
}
