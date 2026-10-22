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
)

type fakeAccounts struct {
	rows []StoredAccount
}

func (f *fakeAccounts) find(id uuid.UUID) int {
	for i, r := range f.rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeAccounts) List(_ context.Context, userID uuid.UUID) ([]domain.Account, error) {
	var out []domain.Account
	for _, r := range f.rows {
		if r.UserID == userID && !r.Archived {
			out = append(out, r.Account)
		}
	}
	return out, nil
}

func (f *fakeAccounts) Insert(_ context.Context, userID uuid.UUID, a NewAccount, _ time.Time) (bool, error) {
	if f.find(a.ID) >= 0 {
		return false, nil
	}
	f.rows = append(f.rows, StoredAccount{
		Account: domain.Account{ID: a.ID, Name: a.Name, Type: a.Type, Currency: a.Currency,
			Balance: a.InitialBalance, IncludeInSavings: a.IncludeInSavings},
		UserID: userID, InitialBalance: a.InitialBalance,
	})
	return true, nil
}

func (f *fakeAccounts) Get(_ context.Context, userID, id uuid.UUID) (StoredAccount, error) {
	i := f.find(id)
	if i < 0 || f.rows[i].UserID != userID {
		return StoredAccount{}, domain.ErrNotFound
	}
	return f.rows[i], nil
}

func (f *fakeAccounts) Update(_ context.Context, userID uuid.UUID, a domain.Account) error {
	i := f.find(a.ID)
	if i < 0 || f.rows[i].UserID != userID || f.rows[i].Archived {
		return domain.ErrNotFound
	}
	f.rows[i].Account = a
	return nil
}

func (f *fakeAccounts) Archive(_ context.Context, userID, id uuid.UUID) error {
	i := f.find(id)
	if i < 0 || f.rows[i].UserID != userID || f.rows[i].Archived {
		return domain.ErrNotFound
	}
	f.rows[i].Archived = true
	return nil
}

type fakeFx struct{ fx domain.Fx }

func (f *fakeFx) Fx(context.Context, uuid.UUID) (domain.Fx, error) { return f.fx, nil }

func newAccountService() (*AccountService, *fakeFx, uuid.UUID) {
	fx := &fakeFx{fx: domain.Fx{Main: "PEN", Secondary: "USD"}}
	s := NewAccountService(&fakeAccounts{}, fx, clock.Fixed{At: time.Date(2026, 10, 1, 12, 0, 0, 0, time.UTC)})
	return s, fx, uuid.New()
}

func ptr[T any](v T) *T { return &v }

func TestANewUserHasCashInTheMainCurrency(t *testing.T) {
	s, _, user := newAccountService()
	require.NoError(t, s.CreateInitial(context.Background(), user, "MXN"))
	list, err := s.List(context.Background(), user)
	require.NoError(t, err)
	require.Len(t, list.Accounts, 1)
	cash := list.Accounts[0]
	assert.Equal(t, "Efectivo", cash.Name)
	assert.Equal(t, domain.AccountCash, cash.Type)
	assert.Equal(t, "MXN", cash.Currency)
	assert.True(t, cash.Balance.IsZero())
	assert.True(t, cash.IncludeInSavings)
}

func TestAnAccountInTheSecondaryNeedsTheRate(t *testing.T) {
	s, fx, user := newAccountService()
	in := CreateAccountInput{Name: "Cuenta Dólares", Type: domain.AccountSavings, Currency: "USD", InitialBalance: ptr("250.00")}
	_, _, err := s.Create(context.Background(), user, in)
	assert.ErrorIs(t, err, domain.ErrFxNotConfigured)

	fx.fx.Rate = ptr(decimal.RequireFromString("3.20"))
	account, created, err := s.Create(context.Background(), user, in)
	require.NoError(t, err)
	assert.True(t, created)
	assert.Equal(t, "250.00", account.Balance.StringFixed(2))

	list, _ := s.List(context.Background(), user)
	assert.Equal(t, "800.00", list.Savings.Totals[0].Amount.StringFixed(2))
}

func TestCreateValidatesInOrderBeforeTheRate(t *testing.T) {
	s, _, user := newAccountService()
	_, _, err := s.Create(context.Background(), user, CreateAccountInput{Name: "", Type: "x", Currency: "XXX", InitialBalance: ptr("-1")})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{
		{Field: "name", Code: domain.CodeNameRequired},
		{Field: "type", Code: domain.CodeTypeInvalid},
		{Field: "currency", Code: domain.CodeCurrencyNotSupported},
		{Field: "initial_balance", Code: domain.CodeAmountInvalid},
	}, problems.Fields)
}

func TestCreatingAccountsWithTheSameIDIsARetry(t *testing.T) {
	s, _, user := newAccountService()
	id := uuid.New()
	in := CreateAccountInput{ID: &id, Name: "Ahorros", Type: domain.AccountSavings, Currency: "PEN", InitialBalance: ptr("100.00")}
	_, created, err := s.Create(context.Background(), user, in)
	require.NoError(t, err)
	assert.True(t, created)

	// "100" y "100.00" son el mismo saldo.
	in.InitialBalance = ptr("100")
	_, created, err = s.Create(context.Background(), user, in)
	require.NoError(t, err)
	assert.False(t, created)

	in.IncludeInSavings = ptr(false)
	_, _, err = s.Create(context.Background(), user, in)
	assert.ErrorIs(t, err, domain.ErrIDTaken)
}

func TestUpdateKeepsCurrencyAndBalance(t *testing.T) {
	s, _, user := newAccountService()
	require.NoError(t, s.CreateInitial(context.Background(), user, "PEN"))
	list, _ := s.List(context.Background(), user)
	id := list.Accounts[0].ID

	updated, err := s.Update(context.Background(), user, id, UpdateAccountInput{
		Name: ptr("Billetera"), Type: ptr(domain.AccountOther), IncludeInSavings: ptr(false),
	})
	require.NoError(t, err)
	assert.Equal(t, "Billetera", updated.Name)
	assert.Equal(t, domain.AccountOther, updated.Type)
	assert.False(t, updated.IncludeInSavings)
	assert.Equal(t, "PEN", updated.Currency)

	list, _ = s.List(context.Background(), user)
	assert.Equal(t, 0, list.Savings.IncludedCount)

	_, err = s.Update(context.Background(), user, id, UpdateAccountInput{Type: ptr(domain.AccountType("wallet"))})
	var problems *domain.ValidationError
	assert.ErrorAs(t, err, &problems)
}

func TestArchivingAnAccountTakesItOutOfSavings(t *testing.T) {
	s, _, user := newAccountService()
	require.NoError(t, s.CreateInitial(context.Background(), user, "PEN"))
	list, _ := s.List(context.Background(), user)
	id := list.Accounts[0].ID

	require.NoError(t, s.Archive(context.Background(), user, id))
	require.NoError(t, s.Archive(context.Background(), user, id))
	list, _ = s.List(context.Background(), user)
	assert.Empty(t, list.Accounts)
	assert.Equal(t, 0, list.Savings.ActiveCount)

	_, err := s.Update(context.Background(), user, id, UpdateAccountInput{Name: ptr("Otra")})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.ErrorIs(t, s.Archive(context.Background(), uuid.New(), id), domain.ErrNotFound)
}
