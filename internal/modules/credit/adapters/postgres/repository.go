// Package postgres implementa los puertos de guardado de credit con las
// consultas de queries.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// DB da la conexión de la transacción en curso, o el pool (tx.Runner).
type DB interface {
	DB(ctx context.Context) tx.DBTX
}

// Cards implementa app.Cards.
type Cards struct{ db DB }

// NewCards crea el repositorio.
func NewCards(db DB) Cards { return Cards{db: db} }

func (r Cards) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// List devuelve las tarjetas activas, en el orden en que se crearon.
func (r Cards) List(ctx context.Context, userID uuid.UUID) ([]domain.Card, error) {
	rows, err := r.q(ctx).ListCards(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Card, 0, len(rows))
	for _, row := range rows {
		out = append(out, domain.Card{ID: row.ID, Name: row.Name, Currency: row.Currency})
	}
	return out, nil
}

// Insert guarda la tarjeta; un id repetido no se pisa.
func (r Cards) Insert(ctx context.Context, userID uuid.UUID, c domain.Card, at time.Time) (bool, error) {
	n, err := r.q(ctx).InsertCard(ctx, sqlcgen.InsertCardParams{ID: c.ID, UserID: userID, Name: c.Name, Currency: c.Currency, CreatedAt: at})
	return n == 1, err
}

// Get busca una tarjeta del usuario, también si está archivada.
func (r Cards) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredCard, error) {
	row, err := r.q(ctx).GetCard(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredCard{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredCard{}, err
	}
	return app.StoredCard{Card: domain.Card{ID: row.ID, Name: row.Name, Currency: row.Currency}, Archived: row.ArchivedAt != nil}, nil
}

// Update cambia nombre y moneda de una tarjeta activa propia.
func (r Cards) Update(ctx context.Context, userID uuid.UUID, c domain.Card) error {
	n, err := r.q(ctx).UpdateCard(ctx, sqlcgen.UpdateCardParams{ID: c.ID, UserID: userID, Name: c.Name, Currency: c.Currency})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Archive archiva una tarjeta activa propia.
func (r Cards) Archive(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).ArchiveCard(ctx, sqlcgen.ArchiveCardParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// CountPending cuenta las compras pendientes de la tarjeta.
func (r Cards) CountPending(ctx context.Context, userID, id uuid.UUID) (int64, error) {
	return r.q(ctx).CountPendingByCard(ctx, sqlcgen.CountPendingByCardParams{CardID: id, UserID: userID})
}

// Purchases implementa app.Purchases.
type Purchases struct{ db DB }

// NewPurchases crea el repositorio.
func NewPurchases(db DB) Purchases { return Purchases{db: db} }

func (r Purchases) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// ListPending devuelve las pendientes con su tarjeta, por fecha límite.
func (r Purchases) ListPending(ctx context.Context, userID uuid.UUID) ([]app.PendingPurchase, error) {
	rows, err := r.q(ctx).ListPendingPurchases(ctx, userID)
	if err != nil {
		return nil, err
	}
	out := make([]app.PendingPurchase, 0, len(rows))
	for _, row := range rows {
		out = append(out, app.PendingPurchase{
			Purchase: domain.Purchase{
				ID: row.ID, CardID: row.CardID, CategoryID: row.CategoryID, Description: row.Description,
				Amount: row.Amount, Currency: row.Currency,
				PurchaseDate: dates.FromTime(row.PurchaseDate), DueDate: dates.FromTime(row.DueDate),
			},
			Card: domain.Card{ID: row.CardID, Name: row.CardName, Currency: row.CardCurrency},
		})
	}
	return out, nil
}

// Insert guarda la compra; un id repetido no se pisa.
func (r Purchases) Insert(ctx context.Context, userID uuid.UUID, p domain.Purchase, at time.Time) (bool, error) {
	n, err := r.q(ctx).InsertPurchase(ctx, sqlcgen.InsertPurchaseParams{
		ID: p.ID, UserID: userID, CardID: p.CardID, CategoryID: p.CategoryID, Description: p.Description,
		Amount: p.Amount, Currency: p.Currency, PurchaseDate: p.PurchaseDate.Time(), DueDate: p.DueDate.Time(), CreatedAt: at,
	})
	return n == 1, err
}

// Get busca una compra del usuario, en cualquier estado.
func (r Purchases) Get(ctx context.Context, userID, id uuid.UUID) (app.StoredPurchase, error) {
	row, err := r.q(ctx).GetPurchase(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && row.UserID != userID) {
		return app.StoredPurchase{}, domain.ErrNotFound
	}
	if err != nil {
		return app.StoredPurchase{}, err
	}
	return app.StoredPurchase{
		Purchase: domain.Purchase{
			ID: row.ID, CardID: row.CardID, CategoryID: row.CategoryID, Description: row.Description,
			Amount: row.Amount, Currency: row.Currency,
			PurchaseDate: dates.FromTime(row.PurchaseDate), DueDate: dates.FromTime(row.DueDate),
		},
		Status:  row.Status,
		Deleted: row.DeletedAt != nil,
	}, nil
}

// Update cambia una compra pendiente propia.
func (r Purchases) Update(ctx context.Context, userID uuid.UUID, p domain.Purchase) error {
	n, err := r.q(ctx).UpdatePurchase(ctx, sqlcgen.UpdatePurchaseParams{
		ID: p.ID, UserID: userID, CardID: p.CardID, CategoryID: p.CategoryID, Description: p.Description,
		Amount: p.Amount, Currency: p.Currency, DueDate: p.DueDate.Time(),
	})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}

// Delete marca como eliminada una compra pendiente propia.
func (r Purchases) Delete(ctx context.Context, userID, id uuid.UUID) error {
	n, err := r.q(ctx).DeletePurchase(ctx, sqlcgen.DeletePurchaseParams{ID: id, UserID: userID})
	if err == nil && n == 0 {
		return domain.ErrNotFound
	}
	return err
}
