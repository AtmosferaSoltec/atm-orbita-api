package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

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

// List devuelve una página de la lista.
func (r Transactions) List(ctx context.Context, userID uuid.UUID, f app.EntryFilter) ([]domain.Entry, error) {
	rows, err := r.q(ctx).ListEntries(ctx, sqlcgen.ListEntriesParams{
		UserID: userID, DateFrom: f.From.Time(), DateTo: f.To.Time(), AccountID: f.AccountID, Pattern: f.Pattern,
		PageLimit: int32(f.Limit), PageOffset: int32(f.Offset),
	})
	if err != nil {
		return nil, err
	}
	out := make([]domain.Entry, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Entry{
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
		})
	}
	return out, nil
}

// Count cuenta las filas del periodo con los mismos filtros.
func (r Transactions) Count(ctx context.Context, userID uuid.UUID, f app.EntryFilter) (int64, error) {
	return r.q(ctx).CountEntries(ctx, sqlcgen.CountEntriesParams{
		UserID: userID, DateFrom: f.From.Time(), DateTo: f.To.Time(), AccountID: f.AccountID, Pattern: f.Pattern,
	})
}
