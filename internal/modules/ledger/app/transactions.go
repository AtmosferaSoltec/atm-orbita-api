package app

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

// StoredTransaction es un movimiento y si está eliminado.
type StoredTransaction struct {
	domain.Transaction
	Deleted bool
}

// EntryFilter elige las filas de la lista.
type EntryFilter struct {
	From, To  dates.LocalDate
	AccountID *uuid.UUID
	// Pattern es un patrón ILIKE ya escapado (domain.SearchPattern), o nil.
	Pattern       *string
	Limit, Offset int
}

// Transactions guarda movimientos. Todo método recibe el usuario.
type Transactions interface {
	// Insert devuelve inserted=false si el id ya existía (de este u otro usuario).
	Insert(ctx context.Context, userID uuid.UUID, t domain.Transaction, at time.Time) (inserted bool, err error)
	// Get devuelve domain.ErrNotFound si no existe o es de otro usuario.
	Get(ctx context.Context, userID, id uuid.UUID) (StoredTransaction, error)
	// Update y Delete devuelven domain.ErrNotFound si no hay uno propio sin eliminar.
	Update(ctx context.Context, userID uuid.UUID, t domain.Transaction) error
	Delete(ctx context.Context, userID, id uuid.UUID) error
	// List devuelve una página de la lista: movimientos y transferencias mezclados.
	List(ctx context.Context, userID uuid.UUID, f EntryFilter) ([]domain.ListEntry, error)
	Count(ctx context.Context, userID uuid.UUID, f EntryFilter) (int64, error)
}

// Transactor corre varias escrituras juntas o ninguna (tx.Runner).
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// PaymentReverter vuelve a pendiente la compra pagada con un egreso que se
// elimina (lo implementa credit; lo cablea main). Corre en la misma transacción.
type PaymentReverter interface {
	RevertPayment(ctx context.Context, userID, purchaseID uuid.UUID) error
}

// TransactionService son los casos de uso de movimientos y de la lista.
type TransactionService struct {
	transactions Transactions
	accounts     Accounts
	categories   Categories
	profile      Profile
	tx           Transactor
	reverter     PaymentReverter
	clock        clock.Clock
}

// NewTransactionService crea el servicio.
func NewTransactionService(transactions Transactions, accounts Accounts, categories Categories, profile Profile, tx Transactor, c clock.Clock) *TransactionService {
	return &TransactionService{transactions: transactions, accounts: accounts, categories: categories, profile: profile, tx: tx, clock: c}
}

// OnPaymentDeleted registra quién vuelve a pendiente la compra de un egreso de pago eliminado.
func (s *TransactionService) OnPaymentDeleted(reverter PaymentReverter) { s.reverter = reverter }

// CreateTransactionInput es Nuevo movimiento. Android no manda la fecha al
// crear: el API usa hoy en la zona del perfil.
type CreateTransactionInput struct {
	ID          *uuid.UUID
	AccountID   *uuid.UUID
	CategoryID  *uuid.UUID
	Amount      *string
	Description string
	OccurredOn  *string
}

// Create guarda un ingreso o un egreso (el tipo sale de la categoría). Un
// reintento con el mismo id y el mismo contenido devuelve el existente.
func (s *TransactionService) Create(ctx context.Context, userID uuid.UUID, in CreateTransactionInput) (domain.Entry, bool, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return domain.Entry{}, false, err
	}
	var problems domain.ValidationError
	t := domain.Transaction{ID: ids.New()}
	t.Amount = domain.ValidateAmount(in.Amount, &problems)
	if in.AccountID == nil {
		problems.Add("account_id", domain.CodeAccountRequired)
	}
	if in.CategoryID == nil {
		problems.Add("category_id", domain.CodeCategoryRequired)
	}
	t.Description = domain.ValidateDescription(in.Description, &problems)
	t.OccurredOn = domain.ValidateDate(in.OccurredOn, today, &problems)
	if err := problems.OrNil(); err != nil {
		return domain.Entry{}, false, err
	}
	if in.ID != nil {
		t.ID = *in.ID
	}
	t.AccountID, t.CategoryID = *in.AccountID, *in.CategoryID

	account, err := usableAccount(ctx, s.accounts, userID, t.AccountID)
	if err != nil {
		return domain.Entry{}, false, err
	}
	category, err := s.usableCategory(ctx, userID, t.CategoryID)
	if err != nil {
		return domain.Entry{}, false, err
	}
	t.Kind = category.Kind

	inserted, err := s.transactions.Insert(ctx, userID, t, s.clock.Now())
	if err != nil {
		return domain.Entry{}, false, err
	}
	if !inserted {
		existing, err := s.transactions.Get(ctx, userID, t.ID)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.Entry{}, false, domain.ErrIDTaken
		}
		if err != nil {
			return domain.Entry{}, false, err
		}
		if existing.Deleted || !sameTransaction(existing.Transaction, t, in.OccurredOn != nil) {
			return domain.Entry{}, false, domain.ErrIDTaken
		}
		t = existing.Transaction
	}
	return entryOf(t, account.Account, category.Category), inserted, nil
}

// sameTransaction compara un reintento. Sin fecha en la petición, la fecha la
// puso el API y no cuenta: el reintento puede llegar al día siguiente.
func sameTransaction(a, b domain.Transaction, compareDate bool) bool {
	return a.AccountID == b.AccountID && a.CategoryID == b.CategoryID && a.Amount.Equal(b.Amount) &&
		a.Description == b.Description && (!compareDate || a.OccurredOn == b.OccurredOn)
}

// UpdateTransactionInput es Editar movimiento: solo cambia lo que viene.
type UpdateTransactionInput struct {
	AccountID   *uuid.UUID
	CategoryID  *uuid.UUID
	Amount      *string
	Description *string
	OccurredOn  *string
}

// Update cambia un movimiento. Una categoría del otro tipo lo convierte de egreso
// en ingreso (o al revés). Una cuenta o categoría archivada solo se puede quedar
// si ya era la suya.
func (s *TransactionService) Update(ctx context.Context, userID, id uuid.UUID, in UpdateTransactionInput) (domain.Entry, error) {
	current, err := s.transactions.Get(ctx, userID, id)
	if err != nil {
		return domain.Entry{}, err
	}
	if current.Deleted {
		return domain.Entry{}, domain.ErrNotFound
	}
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return domain.Entry{}, err
	}
	next := current.Transaction
	var problems domain.ValidationError
	if in.Amount != nil {
		next.Amount = domain.ValidateAmount(in.Amount, &problems)
	}
	if in.Description != nil {
		next.Description = domain.ValidateDescription(*in.Description, &problems)
	}
	if in.OccurredOn != nil {
		next.OccurredOn = domain.ValidateDate(in.OccurredOn, today, &problems)
	}
	if err := problems.OrNil(); err != nil {
		return domain.Entry{}, err
	}
	// El egreso de un pago conserva la categoría y la descripción de su compra:
	// solo cambian la cuenta, el monto y la fecha.
	if current.CreditPurchaseID != nil &&
		(next.Description != current.Description || (in.CategoryID != nil && *in.CategoryID != current.CategoryID)) {
		return domain.Entry{}, domain.ErrPaymentExpenseLocked
	}

	var account StoredAccount
	if in.AccountID != nil && *in.AccountID != current.AccountID {
		next.AccountID = *in.AccountID
		account, err = usableAccount(ctx, s.accounts, userID, next.AccountID)
	} else {
		account, err = s.accounts.Get(ctx, userID, next.AccountID)
	}
	if err != nil {
		return domain.Entry{}, err
	}
	var category StoredCategory
	if in.CategoryID != nil && *in.CategoryID != current.CategoryID {
		next.CategoryID = *in.CategoryID
		category, err = s.usableCategory(ctx, userID, next.CategoryID)
	} else {
		category, err = s.categories.Get(ctx, userID, next.CategoryID)
	}
	if err != nil {
		return domain.Entry{}, err
	}
	next.Kind = category.Kind

	if err := s.transactions.Update(ctx, userID, next); err != nil {
		return domain.Entry{}, err
	}
	return entryOf(next, account.Account, category.Category), nil
}

// Delete elimina (de forma lógica) un movimiento: deja de contar en saldos,
// listas y reportes. Eliminar uno ya eliminado no es un error.
func (s *TransactionService) Delete(ctx context.Context, userID, id uuid.UUID) error {
	current, err := s.transactions.Get(ctx, userID, id)
	if err != nil {
		return err
	}
	if current.Deleted {
		return nil
	}
	if current.CreditPurchaseID == nil || s.reverter == nil {
		return s.transactions.Delete(ctx, userID, id)
	}
	// Eliminar el egreso de un pago devuelve su compra a pendiente, todo junto.
	return s.tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.transactions.Delete(ctx, userID, id); err != nil {
			return err
		}
		return s.reverter.RevertPayment(ctx, userID, *current.CreditPurchaseID)
	})
}

// EntryQuery es la consulta de GET /entries.
type EntryQuery struct {
	From, To  string
	AccountID *uuid.UUID
	Q         string
	Page      int
	PageSize  int
}

// EntryPage es una página de la lista y el total de filas del periodo.
type EntryPage struct {
	Items    []domain.ListEntry
	Page     int
	PageSize int
	Total    int64
}

// Tamaños de página de la lista.
const (
	DefaultPageSize = 50
	MaxPageSize     = 100
	// MaxPage evita que el desplazamiento se desborde; nadie tiene tantas páginas.
	MaxPage = 1_000_000
)

// ListEntries devuelve una página de los movimientos del periodo, del más
// reciente al más antiguo.
func (s *TransactionService) ListEntries(ctx context.Context, userID uuid.UUID, q EntryQuery) (EntryPage, error) {
	var problems domain.ValidationError
	from, to := domain.ValidateRange(q.From, q.To, &problems)
	if q.Page < 1 || q.Page > MaxPage {
		problems.Add("page", domain.CodeInvalidFormat)
	}
	if q.PageSize < 1 || q.PageSize > MaxPageSize {
		problems.Add("page_size", domain.CodeInvalidFormat)
	}
	if err := problems.OrNil(); err != nil {
		return EntryPage{}, err
	}
	filter := EntryFilter{
		From: from, To: to, AccountID: q.AccountID, Pattern: domain.SearchPattern(q.Q),
		Limit: q.PageSize, Offset: (q.Page - 1) * q.PageSize,
	}
	items, err := s.transactions.List(ctx, userID, filter)
	if err != nil {
		return EntryPage{}, err
	}
	total, err := s.transactions.Count(ctx, userID, filter)
	if err != nil {
		return EntryPage{}, err
	}
	return EntryPage{Items: items, Page: q.Page, PageSize: q.PageSize, Total: total}, nil
}

func usableAccount(ctx context.Context, accounts Accounts, userID, id uuid.UUID) (StoredAccount, error) {
	account, err := accounts.Get(ctx, userID, id)
	if err == nil && account.Archived {
		return StoredAccount{}, domain.ErrAccountArchived
	}
	return account, err
}

func (s *TransactionService) usableCategory(ctx context.Context, userID, id uuid.UUID) (StoredCategory, error) {
	category, err := s.categories.Get(ctx, userID, id)
	if err == nil && category.Archived {
		return StoredCategory{}, domain.ErrCategoryArchived
	}
	return category, err
}

func entryOf(t domain.Transaction, a domain.Account, c domain.Category) domain.Entry {
	return domain.Entry{
		Transaction: t,
		Account:     domain.EntryAccount{ID: a.ID, Name: a.Name, Type: a.Type, Currency: a.Currency},
		Category:    c,
	}
}

// RecentEntries son los limit movimientos y transferencias más recientes de los
// últimos 10 años (el periodo más largo de la lista), para Inicio.
func (s *TransactionService) RecentEntries(ctx context.Context, userID uuid.UUID, limit int) ([]domain.ListEntry, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return nil, err
	}
	from := dates.FromTime(today.Time().AddDate(-domain.MaxRangeYears, 0, 1))
	return s.transactions.List(ctx, userID, EntryFilter{From: from, To: today, Limit: limit})
}

// PaymentExpense es el egreso que crea el pago de una compra con tarjeta.
type PaymentExpense struct {
	PurchaseID  uuid.UUID
	AccountID   uuid.UUID
	CategoryID  uuid.UUID
	Description string
	Amount      decimal.Decimal
	OccurredOn  dates.LocalDate
}

// CreatePaymentExpense crea el egreso del pago de una compra, en la transacción
// en curso (la abre credit). La cuenta tiene que estar activa; la categoría es la
// de la compra aunque se haya archivado después: el pago es de esa compra.
func (s *TransactionService) CreatePaymentExpense(ctx context.Context, userID uuid.UUID, in PaymentExpense) (domain.Entry, error) {
	account, err := usableAccount(ctx, s.accounts, userID, in.AccountID)
	if err != nil {
		return domain.Entry{}, err
	}
	category, err := s.categories.Get(ctx, userID, in.CategoryID)
	if err != nil {
		return domain.Entry{}, err
	}
	purchase := in.PurchaseID
	t := domain.Transaction{
		ID: ids.New(), AccountID: account.ID, CategoryID: category.ID, Kind: category.Kind,
		Amount: in.Amount, OccurredOn: in.OccurredOn, Description: in.Description, CreditPurchaseID: &purchase,
	}
	if _, err := s.transactions.Insert(ctx, userID, t, s.clock.Now()); err != nil {
		return domain.Entry{}, err
	}
	return entryOf(t, account.Account, category.Category), nil
}
