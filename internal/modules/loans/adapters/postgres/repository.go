// Package postgres implements normalized loan persistence using owner-scoped sqlc queries.
package postgres

import (
	"context"
	"errors"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"time"
)

type DB interface{ DB(context.Context) tx.DBTX }
type Repository struct{ db DB }

func New(db DB) Repository                                  { return Repository{db} }
func (r Repository) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }
func notFound(err error) error {
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}
func (r Repository) Lock(ctx context.Context, user, id uuid.UUID) error {
	_, err := r.q(ctx).LockLoanDebtor(ctx, sqlcgen.LockLoanDebtorParams{UserID: user, ID: id})
	return notFound(err)
}
func (r Repository) List(ctx context.Context, user uuid.UUID) ([]domain.Debtor, error) {
	rows, err := r.q(ctx).ListLoanDebtors(ctx, user)
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, user, rows)
}
func (r Repository) Get(ctx context.Context, user, id uuid.UUID) (domain.Debtor, error) {
	row, err := r.q(ctx).GetLoanDebtor(ctx, sqlcgen.GetLoanDebtorParams{UserID: user, ID: id})
	if err != nil {
		return domain.Debtor{}, notFound(err)
	}
	items, err := r.hydrate(ctx, user, []sqlcgen.LoanDebtor{row})
	if err != nil {
		return domain.Debtor{}, err
	}
	return items[0], nil
}
func (r Repository) Shared(ctx context.Context, token string) (domain.Debtor, error) {
	row, err := r.q(ctx).SharedLoanDebtor(ctx, &token)
	if err != nil {
		return domain.Debtor{}, notFound(err)
	}
	items, err := r.hydrate(ctx, row.UserID, []sqlcgen.LoanDebtor{row})
	if err != nil {
		return domain.Debtor{}, err
	}
	return items[0], nil
}
func (r Repository) hydrate(ctx context.Context, user uuid.UUID, rows []sqlcgen.LoanDebtor) ([]domain.Debtor, error) {
	result := make([]domain.Debtor, 0, len(rows))
	ids := make([]uuid.UUID, 0, len(rows))
	index := map[uuid.UUID]int{}
	for _, d := range rows {
		index[d.ID] = len(result)
		ids = append(ids, d.ID)
		result = append(result, domain.Debtor{ID: d.ID, Name: d.Name, Currency: d.Currency, ShareToken: d.ShareToken, CreatedAt: d.CreatedAt, Movements: []domain.Movement{}})
	}
	if len(rows) == 0 {
		return result, nil
	}
	movements, err := r.q(ctx).ListLoanMovements(ctx, sqlcgen.ListLoanMovementsParams{UserID: user, DebtorIds: ids})
	if err != nil {
		return nil, err
	}
	plans, err := r.q(ctx).ListLoanPlans(ctx, sqlcgen.ListLoanPlansParams{UserID: user, DebtorIds: ids})
	if err != nil {
		return nil, err
	}
	byMovement := map[uuid.UUID][]domain.Plan{}
	for _, p := range plans {
		byMovement[p.MovementID] = append(byMovement[p.MovementID], domain.Plan{Month: p.Month.Format("2006-01"), Amount: money.NewAmount(p.Amount)})
	}
	for _, m := range movements {
		month := ""
		if m.AppliedMonth != nil {
			month = m.AppliedMonth.Format("2006-01")
		}
		plan := byMovement[m.ID]
		if plan == nil {
			plan = []domain.Plan{}
		}
		i := index[m.DebtorID]
		result[i].Movements = append(result[i].Movements, domain.Movement{ID: m.ID, Kind: m.Kind, Amount: money.NewAmount(m.Amount), Date: dates.FromTime(m.OccurredOn), AppliedMonth: month, Title: m.Title, Note: m.Note, Plan: plan, CreatedAt: m.CreatedAt})
	}
	return result, nil
}
func (r Repository) Insert(ctx context.Context, user uuid.UUID, d domain.Debtor) (bool, error) {
	n, err := r.q(ctx).InsertLoanDebtor(ctx, sqlcgen.InsertLoanDebtorParams{ID: d.ID, UserID: user, Name: d.Name, Currency: d.Currency, CreatedAt: d.CreatedAt})
	return n == 1, err
}
func (r Repository) Rename(ctx context.Context, user, id uuid.UUID, name string) error {
	return r.q(ctx).RenameLoanDebtor(ctx, sqlcgen.RenameLoanDebtorParams{UserID: user, ID: id, Name: name})
}
func (r Repository) Delete(ctx context.Context, user, id uuid.UUID) error {
	return r.q(ctx).DeleteLoanDebtor(ctx, sqlcgen.DeleteLoanDebtorParams{UserID: user, ID: id})
}
func optionalMonth(month string) *time.Time {
	if month == "" {
		return nil
	}
	t, _ := time.Parse("2006-01", month)
	return &t
}
func (r Repository) InsertMovement(ctx context.Context, user, id uuid.UUID, m domain.Movement) (bool, error) {
	n, err := r.q(ctx).InsertLoanMovement(ctx, sqlcgen.InsertLoanMovementParams{ID: m.ID, DebtorID: id, UserID: user, Kind: m.Kind, Amount: m.Amount.Decimal(), OccurredOn: m.Date.Time(), AppliedMonth: optionalMonth(m.AppliedMonth), Title: m.Title, Note: m.Note, CreatedAt: m.CreatedAt})
	return n == 1, err
}
func (r Repository) UpdateMovement(ctx context.Context, user, id uuid.UUID, m domain.Movement) error {
	return r.q(ctx).UpdateLoanMovement(ctx, sqlcgen.UpdateLoanMovementParams{ID: m.ID, DebtorID: id, UserID: user, Amount: m.Amount.Decimal(), OccurredOn: m.Date.Time(), AppliedMonth: optionalMonth(m.AppliedMonth), Title: m.Title, Note: m.Note})
}
func (r Repository) DeleteMovement(ctx context.Context, user, id, movement uuid.UUID) error {
	return r.q(ctx).DeleteLoanMovement(ctx, sqlcgen.DeleteLoanMovementParams{ID: movement, DebtorID: id, UserID: user})
}
func (r Repository) SavePlan(ctx context.Context, user, id uuid.UUID, m domain.Movement) error {
	q := r.q(ctx)
	if err := q.DeleteLoanPlan(ctx, sqlcgen.DeleteLoanPlanParams{UserID: user, DebtorID: id, MovementID: m.ID}); err != nil {
		return err
	}
	for _, p := range m.Plan {
		month, _ := time.Parse("2006-01", p.Month)
		if err := q.InsertLoanPlan(ctx, sqlcgen.InsertLoanPlanParams{MovementID: m.ID, DebtorID: id, UserID: user, Month: month, Amount: p.Amount.Decimal()}); err != nil {
			return err
		}
	}
	return nil
}
func (r Repository) SetShare(ctx context.Context, user, id uuid.UUID, token *string) error {
	return r.q(ctx).SetLoanShare(ctx, sqlcgen.SetLoanShareParams{UserID: user, ID: id, ShareToken: token})
}
