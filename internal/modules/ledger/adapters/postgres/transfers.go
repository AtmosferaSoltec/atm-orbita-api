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

// entryTransfer es el entry_type de una transferencia en ListEntryKeys.
const entryTransfer = "transfer"

// Transfers implementa app.Transfers.
type Transfers struct {
	db DB
}

// NewTransfers crea el repositorio.
func NewTransfers(db DB) Transfers { return Transfers{db: db} }

func (r Transfers) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// Insert guarda la transferencia; un id repetido no se pisa.
func (r Transfers) Insert(ctx context.Context, userID uuid.UUID, t domain.Transfer, at time.Time) (bool, error) {
	n, err := r.q(ctx).InsertTransfer(ctx, sqlcgen.InsertTransferParams{
		ID: t.ID, UserID: userID, FromAccountID: t.FromAccountID, ToAccountID: t.ToAccountID,
		FromAmount: t.FromAmount, ToAmount: t.ToAmount, ExchangeRate: nullRate(t.ExchangeRate),
		OccurredOn: t.OccurredOn.Time(), Note: t.Note, CreatedAt: at,
	})
	return n == 1, err
}

// Get busca una transferencia del usuario, también si está eliminada.
func (r Transfers) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredTransfer, error) {
	row, err := r.q(ctx).GetTransfer(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredTransfer{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredTransfer{}, err
	}
	return app.StoredTransfer{
		Transfer: domain.Transfer{
			ID: row.ID, FromAccountID: row.FromAccountID, ToAccountID: row.ToAccountID,
			FromAmount: row.FromAmount, ToAmount: row.ToAmount, ExchangeRate: rateOf(row.ExchangeRate),
			OccurredOn: dates.FromTime(row.OccurredOn), Note: row.Note,
		},
		Deleted: row.DeletedAt != nil,
	}, nil
}

// Update reemplaza los datos de una transferencia propia sin eliminar.
func (r Transfers) Update(ctx context.Context, userID uuid.UUID, t domain.Transfer) error {
	n, err := r.q(ctx).UpdateTransfer(ctx, sqlcgen.UpdateTransferParams{
		ID: t.ID, UserID: userID, FromAccountID: t.FromAccountID, ToAccountID: t.ToAccountID,
		FromAmount: t.FromAmount, ToAmount: t.ToAmount, ExchangeRate: nullRate(t.ExchangeRate),
		OccurredOn: t.OccurredOn.Time(), Note: t.Note,
	})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Delete marca la transferencia como eliminada.
func (r Transfers) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).DeleteTransfer(ctx, sqlcgen.DeleteTransferParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

func nullRate(rate *decimal.Decimal) decimal.NullDecimal {
	if rate == nil {
		return decimal.NullDecimal{}
	}
	return decimal.NullDecimal{Decimal: *rate, Valid: true}
}

func rateOf(rate decimal.NullDecimal) *decimal.Decimal {
	if !rate.Valid {
		return nil
	}
	return &rate.Decimal
}
