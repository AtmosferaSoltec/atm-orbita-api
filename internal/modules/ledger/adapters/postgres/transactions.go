package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// Transactions implementa app.Transactions.
type Transactions struct {
	db DB
}

// NewTransactions crea el repositorio.
func NewTransactions(db DB) Transactions { return Transactions{db: db} }

func (r Transactions) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// Insert guarda el movimiento; un id repetido no se pisa.
func (r Transactions) Insert(ctx context.Context, userID uuid.UUID, t domain.Transaction, at time.Time) (bool, error) {
	n, err := r.q(ctx).InsertTransaction(ctx, sqlcgen.InsertTransactionParams{
		ID: t.ID, UserID: userID, AccountID: t.AccountID, CategoryID: t.CategoryID, Kind: string(t.Kind),
		Amount: t.Amount, OccurredOn: t.OccurredOn.Time(), Description: t.Description, CreatedAt: at,
	})
	return n == 1, err
}

// Get busca un movimiento del usuario, también si está eliminado.
func (r Transactions) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredTransaction, error) {
	row, err := r.q(ctx).GetTransaction(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredTransaction{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredTransaction{}, err
	}
	return app.StoredTransaction{
		Transaction: domain.Transaction{
			ID: row.ID, AccountID: row.AccountID, CategoryID: row.CategoryID, Kind: domain.Kind(row.Kind),
			Amount: row.Amount, OccurredOn: dates.FromTime(row.OccurredOn), Description: row.Description,
		},
		Deleted: row.DeletedAt != nil,
	}, nil
}

// Update reemplaza los datos de un movimiento propio sin eliminar.
func (r Transactions) Update(ctx context.Context, userID uuid.UUID, t domain.Transaction) error {
	n, err := r.q(ctx).UpdateTransaction(ctx, sqlcgen.UpdateTransactionParams{
		ID: t.ID, UserID: userID, AccountID: t.AccountID, CategoryID: t.CategoryID, Kind: string(t.Kind),
		Amount: t.Amount, OccurredOn: t.OccurredOn.Time(), Description: t.Description,
	})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Delete marca el movimiento como eliminado.
func (r Transactions) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).DeleteTransaction(ctx, sqlcgen.DeleteTransactionParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// List devuelve una página de la lista: primero qué filas y en qué orden (una
// consulta sobre las dos tablas), luego los detalles de cada tipo.
func (r Transactions) List(ctx context.Context, userID uuid.UUID, f app.EntryFilter) ([]domain.ListEntry, error) {
	q := r.q(ctx)
	keys, err := q.ListEntryKeys(ctx, sqlcgen.ListEntryKeysParams{
		UserID: userID, DateFrom: f.From.Time(), DateTo: f.To.Time(), AccountID: f.AccountID, Pattern: f.Pattern,
		PageLimit: int32(f.Limit), PageOffset: int32(f.Offset),
	})
	if err != nil {
		return nil, err
	}
	var movementIDs, transferIDs []uuid.UUID
	for _, k := range keys {
		if k.EntryType == entryTransfer {
			transferIDs = append(transferIDs, k.ID)
		} else {
			movementIDs = append(movementIDs, k.ID)
		}
	}
	movements := map[uuid.UUID]*domain.Entry{}
	if len(movementIDs) > 0 {
		rows, err := q.ListTransactionsByIDs(ctx, sqlcgen.ListTransactionsByIDsParams{UserID: userID, Ids: movementIDs})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			movements[row.ID] = &domain.Entry{
				Transaction: domain.Transaction{
					ID: row.ID, AccountID: row.AccountID, CategoryID: row.CategoryID, Kind: domain.Kind(row.Kind),
					Amount: row.Amount, OccurredOn: dates.FromTime(row.OccurredOn), Description: row.Description,
				},
				Account: domain.EntryAccount{
					ID: row.AccountID, Name: row.AccountName, Type: domain.AccountType(row.AccountType), Currency: row.AccountCurrency,
				},
				Category: domain.Category{
					ID: row.CategoryID, Name: row.CategoryName, Kind: domain.Kind(row.CategoryKind), Color: row.CategoryColor,
				},
			}
		}
	}
	transfers := map[uuid.UUID]*domain.TransferEntry{}
	if len(transferIDs) > 0 {
		rows, err := q.ListTransfersByIDs(ctx, sqlcgen.ListTransfersByIDsParams{UserID: userID, Ids: transferIDs})
		if err != nil {
			return nil, err
		}
		for _, row := range rows {
			transfers[row.ID] = &domain.TransferEntry{
				Transfer: domain.Transfer{
					ID: row.ID, FromAccountID: row.FromID, ToAccountID: row.ToID, FromAmount: row.FromAmount,
					ToAmount: row.ToAmount, ExchangeRate: rateOf(row.ExchangeRate),
					OccurredOn: dates.FromTime(row.OccurredOn), Note: row.Note,
				},
				From: domain.EntryAccount{ID: row.FromID, Name: row.FromName, Type: domain.AccountType(row.FromType), Currency: row.FromCurrency},
				To:   domain.EntryAccount{ID: row.ToID, Name: row.ToName, Type: domain.AccountType(row.ToType), Currency: row.ToCurrency},
			}
		}
	}
	out := make([]domain.ListEntry, 0, len(keys))
	for _, k := range keys {
		switch {
		case movements[k.ID] != nil:
			out = append(out, domain.ListEntry{Movement: movements[k.ID]})
		case transfers[k.ID] != nil:
			out = append(out, domain.ListEntry{Transfer: transfers[k.ID]})
		}
	}
	return out, nil
}

// Count cuenta las filas del periodo con los mismos filtros.
func (r Transactions) Count(ctx context.Context, userID uuid.UUID, f app.EntryFilter) (int64, error) {
	return r.q(ctx).CountEntries(ctx, sqlcgen.CountEntriesParams{
		UserID: userID, DateFrom: f.From.Time(), DateTo: f.To.Time(), AccountID: f.AccountID, Pattern: f.Pattern,
	})
}

// TotalByCategory es la suma de los movimientos de una categoría en una moneda.
type TotalByCategory struct {
	Currency  string
	Kind      domain.Kind
	Category  domain.Category
	Total     decimal.Decimal
	Movements int64
}

// TotalsByCategory suma en la base los movimientos del periodo por moneda de la
// cuenta, tipo y categoría.
func (r Transactions) TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]TotalByCategory, error) {
	rows, err := r.q(ctx).TotalsByCategory(ctx, sqlcgen.TotalsByCategoryParams{UserID: userID, DateFrom: from.Time(), DateTo: to.Time()})
	if err != nil {
		return nil, err
	}
	out := make([]TotalByCategory, 0, len(rows))
	for _, row := range rows {
		kind := domain.Kind(row.Kind)
		out = append(out, TotalByCategory{
			Currency:  row.Currency,
			Kind:      kind,
			Category:  domain.Category{ID: row.CategoryID, Name: row.CategoryName, Kind: kind, Color: row.CategoryColor},
			Total:     row.Total,
			Movements: row.Movements,
		})
	}
	return out, nil
}
