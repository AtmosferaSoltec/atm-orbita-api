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

type fakeTransactions struct {
	rows  []StoredTransaction
	users map[uuid.UUID]uuid.UUID
}

func (f *fakeTransactions) find(id uuid.UUID) int {
	for i, r := range f.rows {
		if r.ID == id {
			return i
		}
	}
	return -1
}

func (f *fakeTransactions) Insert(_ context.Context, userID uuid.UUID, t domain.Transaction, _ time.Time) (bool, error) {
	if f.find(t.ID) >= 0 {
		return false, nil
	}
	f.rows = append(f.rows, StoredTransaction{Transaction: t})
	f.users[t.ID] = userID
	return true, nil
}

func (f *fakeTransactions) Get(_ context.Context, userID, id uuid.UUID) (StoredTransaction, error) {
	i := f.find(id)
	if i < 0 || f.users[id] != userID {
		return StoredTransaction{}, domain.ErrNotFound
	}
	return f.rows[i], nil
}

func (f *fakeTransactions) Update(_ context.Context, _ uuid.UUID, t domain.Transaction) error {
	f.rows[f.find(t.ID)].Transaction = t
	return nil
}

func (f *fakeTransactions) Delete(_ context.Context, _ uuid.UUID, id uuid.UUID) error {
	f.rows[f.find(id)].Deleted = true
	return nil
}

func (f *fakeTransactions) List(context.Context, uuid.UUID, EntryFilter) ([]domain.ListEntry, error) {
	return nil, nil
}

func (f *fakeTransactions) Count(context.Context, uuid.UUID, EntryFilter) (int64, error) {
	return 0, nil
}

type txFixture struct {
	service              *TransactionService
	transactions         *fakeTransactions
	user                 uuid.UUID
	cash, archivedAcct   uuid.UUID
	food, salary, oldCat uuid.UUID
}

func newTxFixture(t *testing.T) txFixture {
	t.Helper()
	ctx := context.Background()
	user := uuid.New()
	accounts, categories := &fakeAccounts{}, &fakeCategories{}
	f := txFixture{user: user, cash: uuid.New(), archivedAcct: uuid.New(), food: uuid.New(), salary: uuid.New(), oldCat: uuid.New()}
	for _, id := range []uuid.UUID{f.cash, f.archivedAcct} {
		_, err := accounts.Insert(ctx, user, NewAccount{ID: id, Name: id.String()[:8], Type: domain.AccountCash, Currency: "PEN"}, time.Time{})
		require.NoError(t, err)
	}
	require.NoError(t, accounts.Archive(ctx, user, f.archivedAcct))
	for _, c := range []domain.Category{
		{ID: f.food, Name: "Alimentación", Kind: domain.KindExpense, Color: "#0E7490"},
		{ID: f.salary, Name: "Sueldo", Kind: domain.KindIncome, Color: "#0B7A5A"},
		{ID: f.oldCat, Name: "Vieja", Kind: domain.KindExpense, Color: "#64748B"},
	} {
		_, err := categories.Insert(ctx, user, c, time.Time{})
		require.NoError(t, err)
	}
	require.NoError(t, categories.Archive(ctx, user, f.oldCat))
	f.transactions = &fakeTransactions{users: map[uuid.UUID]uuid.UUID{}}
	profile := &fakeFx{fx: domain.Fx{Main: "PEN", Secondary: "USD"}, today: dates.New(2026, time.October, 2)}
	f.service = NewTransactionService(f.transactions, accounts, categories, profile, runInline{}, clock.Fixed{At: time.Date(2026, 10, 3, 1, 0, 0, 0, time.UTC)})
	return f
}

func TestCreateValidatesInTheOrderOfTheApp(t *testing.T) {
	f := newTxFixture(t)
	_, _, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{
		Amount: ptr("0"), OccurredOn: ptr("2026-10-03"),
	})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{
		{Field: "amount", Code: domain.CodeAmountRequired},
		{Field: "account_id", Code: domain.CodeAccountRequired},
		{Field: "category_id", Code: domain.CodeCategoryRequired},
		{Field: "occurred_on", Code: domain.CodeFutureDate},
	}, problems.Fields)
}

func TestANewMovementTakesTodayOfTheProfileAndTheKindOfItsCategory(t *testing.T) {
	f := newTxFixture(t)
	entry, created, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{
		AccountID: &f.cash, CategoryID: &f.salary, Amount: ptr("3500.00"), Description: " Sueldo ",
	})
	require.NoError(t, err)
	assert.True(t, created)
	// El reloj marca el 3 de octubre en UTC, pero en la zona del perfil aún es 2.
	assert.Equal(t, "2026-10-02", entry.OccurredOn.String())
	assert.Equal(t, domain.KindIncome, entry.Kind)
	assert.Equal(t, "Sueldo", entry.Description)
	assert.Equal(t, "PEN", entry.Account.Currency)
}

func TestArchivedAccountsAndCategoriesAreNotUsedForNewMovements(t *testing.T) {
	f := newTxFixture(t)
	_, _, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &f.archivedAcct, CategoryID: &f.food, Amount: ptr("1.00")})
	assert.ErrorIs(t, err, domain.ErrAccountArchived)
	_, _, err = f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &f.cash, CategoryID: &f.oldCat, Amount: ptr("1.00")})
	assert.ErrorIs(t, err, domain.ErrCategoryArchived)
	other := uuid.New()
	_, _, err = f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &other, CategoryID: &f.food, Amount: ptr("1.00")})
	assert.ErrorIs(t, err, domain.ErrNotFound)
}

func TestChangingTheCategoryCanTurnAnExpenseIntoAnIncome(t *testing.T) {
	f := newTxFixture(t)
	entry, _, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &f.cash, CategoryID: &f.food, Amount: ptr("45.52")})
	require.NoError(t, err)
	updated, err := f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{CategoryID: &f.salary, Amount: ptr("50.00")})
	require.NoError(t, err)
	assert.Equal(t, domain.KindIncome, updated.Kind)
	assert.True(t, updated.Amount.Equal(decimal.RequireFromString("50")))

	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{CategoryID: &f.oldCat})
	assert.ErrorIs(t, err, domain.ErrCategoryArchived)
	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{OccurredOn: ptr("2026-10-03")})
	var problems *domain.ValidationError
	assert.ErrorAs(t, err, &problems)
}

func TestDeletingTwiceIsFineAndAnotherUserGets404(t *testing.T) {
	f := newTxFixture(t)
	entry, _, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &f.cash, CategoryID: &f.food, Amount: ptr("10.00")})
	require.NoError(t, err)
	require.NoError(t, f.service.Delete(context.Background(), f.user, entry.ID))
	require.NoError(t, f.service.Delete(context.Background(), f.user, entry.ID))
	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{Amount: ptr("1.00")})
	assert.ErrorIs(t, err, domain.ErrNotFound)
	assert.ErrorIs(t, f.service.Delete(context.Background(), uuid.New(), entry.ID), domain.ErrNotFound)
}

func TestARetryWithoutDateMatchesEvenOnAnotherDay(t *testing.T) {
	f := newTxFixture(t)
	id := uuid.New()
	in := CreateTransactionInput{ID: &id, AccountID: &f.cash, CategoryID: &f.food, Amount: ptr("18.50"), Description: "Almuerzo"}
	_, created, err := f.service.Create(context.Background(), f.user, in)
	require.NoError(t, err)
	assert.True(t, created)
	f.transactions.rows[0].OccurredOn = dates.New(2026, time.October, 1)

	_, created, err = f.service.Create(context.Background(), f.user, in)
	require.NoError(t, err)
	assert.False(t, created)
	in.Amount = ptr("18.00")
	_, _, err = f.service.Create(context.Background(), f.user, in)
	assert.ErrorIs(t, err, domain.ErrIDTaken)
}

func TestListEntriesChecksTheRangeAndThePage(t *testing.T) {
	f := newTxFixture(t)
	_, err := f.service.ListEntries(context.Background(), f.user, EntryQuery{From: "2026-10-01", To: "2026-09-01", Page: 0, PageSize: 101})
	var problems *domain.ValidationError
	require.ErrorAs(t, err, &problems)
	assert.Equal(t, []domain.FieldError{
		{Field: "to", Code: domain.CodeRangeInvalid},
		{Field: "page", Code: domain.CodeInvalidFormat},
		{Field: "page_size", Code: domain.CodeInvalidFormat},
	}, problems.Fields)
}

// runInline corre fn sin transacción: los fakes no la necesitan.
type runInline struct{}

func (runInline) InTx(ctx context.Context, fn func(ctx context.Context) error) error { return fn(ctx) }

type fakeReverter struct {
	reverted []uuid.UUID
	err      error
}

func (r *fakeReverter) RevertPayment(_ context.Context, _, purchaseID uuid.UUID) error {
	r.reverted = append(r.reverted, purchaseID)
	return r.err
}

func TestThePaymentExpenseOnlyChangesAccountAmountAndDate(t *testing.T) {
	f := newTxFixture(t)
	purchase := uuid.New()
	entry, err := f.service.CreatePaymentExpense(context.Background(), f.user, PaymentExpense{
		PurchaseID: purchase, AccountID: f.cash, CategoryID: f.food, Description: "Pasajes",
		Amount: decimal.RequireFromString("240"), OccurredOn: dates.New(2026, time.October, 2),
	})
	require.NoError(t, err)
	assert.Equal(t, &purchase, entry.CreditPurchaseID)
	assert.Equal(t, domain.KindExpense, entry.Kind)

	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{Amount: ptr("238.50"), OccurredOn: ptr("2026-10-01")})
	assert.NoError(t, err)
	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{Description: ptr("Otra cosa")})
	assert.ErrorIs(t, err, domain.ErrPaymentExpenseLocked)
	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{CategoryID: &f.salary})
	assert.ErrorIs(t, err, domain.ErrPaymentExpenseLocked)
	_, err = f.service.Update(context.Background(), f.user, entry.ID, UpdateTransactionInput{Description: ptr(" Pasajes "), CategoryID: &f.food})
	assert.NoError(t, err, "la misma descripción y categoría no cuentan como cambio")
}

func TestDeletingThePaymentExpenseRevertsItsPurchase(t *testing.T) {
	f := newTxFixture(t)
	reverter := &fakeReverter{}
	f.service.OnPaymentDeleted(reverter)
	purchase := uuid.New()
	entry, err := f.service.CreatePaymentExpense(context.Background(), f.user, PaymentExpense{
		PurchaseID: purchase, AccountID: f.cash, CategoryID: f.food, Amount: decimal.RequireFromString("10"),
		OccurredOn: dates.New(2026, time.October, 2),
	})
	require.NoError(t, err)
	require.NoError(t, f.service.Delete(context.Background(), f.user, entry.ID))
	assert.Equal(t, []uuid.UUID{purchase}, reverter.reverted)

	// Un movimiento común no toca ninguna compra.
	plain, _, err := f.service.Create(context.Background(), f.user, CreateTransactionInput{AccountID: &f.cash, CategoryID: &f.food, Amount: ptr("1.00")})
	require.NoError(t, err)
	require.NoError(t, f.service.Delete(context.Background(), f.user, plain.ID))
	assert.Len(t, reverter.reverted, 1)

	// Un pago no se crea desde una cuenta archivada.
	_, err = f.service.CreatePaymentExpense(context.Background(), f.user, PaymentExpense{
		PurchaseID: uuid.New(), AccountID: f.archivedAcct, CategoryID: f.food, Amount: decimal.RequireFromString("1"),
	})
	assert.ErrorIs(t, err, domain.ErrAccountArchived)
}
