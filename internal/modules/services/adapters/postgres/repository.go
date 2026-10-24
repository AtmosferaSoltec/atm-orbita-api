// Package postgres persists each subscription and its members within one transaction.
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

type DB interface{ DB(context.Context) tx.DBTX }
type Repository struct{ db DB }

func New(db DB) Repository                                  { return Repository{db} }
func (r Repository) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }
func (r Repository) Lock(ctx context.Context, userID, id uuid.UUID) error {
	_, err := r.q(ctx).LockService(ctx, sqlcgen.LockServiceParams{UserID: userID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.ErrNotFound
	}
	return err
}

func (r Repository) List(ctx context.Context, userID uuid.UUID) ([]domain.Service, error) {
	rows, err := r.q(ctx).ListServices(ctx, userID)
	if err != nil {
		return nil, err
	}
	return r.hydrate(ctx, userID, rows)
}

func (r Repository) Get(ctx context.Context, userID, id uuid.UUID) (domain.Service, error) {
	row, err := r.q(ctx).GetService(ctx, sqlcgen.GetServiceParams{UserID: userID, ID: id})
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Service{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.Service{}, err
	}
	out, err := r.hydrate(ctx, userID, []sqlcgen.Service{row})
	if err != nil {
		return domain.Service{}, err
	}
	return out[0], nil
}

func (r Repository) hydrate(ctx context.Context, userID uuid.UUID, rows []sqlcgen.Service) ([]domain.Service, error) {
	out := make([]domain.Service, 0, len(rows))
	if len(rows) == 0 {
		return out, nil
	}
	q := r.q(ctx)
	members, err := q.ListMembers(ctx, userID)
	if err != nil {
		return nil, err
	}
	payments, err := q.ListServicePayments(ctx, userID)
	if err != nil {
		return nil, err
	}
	byID := map[uuid.UUID]*domain.Service{}
	for _, row := range rows {
		var ended *dates.LocalDate
		if row.EndedOn != nil {
			d := dates.FromTime(*row.EndedOn)
			ended = &d
		}
		out = append(out, domain.Service{ID: row.ID, Name: row.Name, Color: row.Color, Currency: row.Currency, Amount: money.NewAmount(row.Amount), BillingDay: int(row.BillingDay), StartedOn: dates.FromTime(row.StartedOn), Status: row.Status, EndedOn: ended, Note: row.Note, Shared: row.Shared, CreatedAt: row.CreatedAt, Members: []domain.Member{}, Payments: []domain.Payment{}})
	}
	for i := range out {
		byID[out[i].ID] = &out[i]
	}
	for _, m := range members {
		if s := byID[m.ServiceID]; s != nil {
			s.Members = append(s.Members, domain.Member{ID: m.ID, Name: m.Name, Amount: money.NewAmount(m.Amount), Since: m.SinceMonth.Format("2006-01")})
		}
	}
	for _, p := range payments {
		if s := byID[p.ServiceID]; s != nil {
			s.Payments = append(s.Payments, domain.Payment{ID: p.ID, MemberID: p.MemberID, Month: p.Month.Format("2006-01"), PaidOn: dates.FromTime(p.PaidOn), Amount: money.NewAmount(p.Amount)})
		}
	}
	return out, nil
}

func (r Repository) Insert(ctx context.Context, userID uuid.UUID, s domain.Service) (bool, error) {
	n, err := r.q(ctx).InsertService(ctx, sqlcgen.InsertServiceParams{ID: s.ID, UserID: userID, Name: s.Name, Color: s.Color, Currency: s.Currency, Amount: s.Amount.Decimal(), BillingDay: int16(s.BillingDay), StartedOn: s.StartedOn.Time(), Status: s.Status, EndedOn: optionalDate(s.EndedOn), Note: s.Note, Shared: s.Shared, CreatedAt: s.CreatedAt})
	return n == 1, err
}

func (r Repository) Update(ctx context.Context, userID uuid.UUID, s domain.Service) error {
	return r.q(ctx).UpdateService(ctx, sqlcgen.UpdateServiceParams{ID: s.ID, UserID: userID, Name: s.Name, Color: s.Color, Currency: s.Currency, Amount: s.Amount.Decimal(), BillingDay: int16(s.BillingDay), StartedOn: s.StartedOn.Time(), Status: s.Status, EndedOn: optionalDate(s.EndedOn), Note: s.Note, Shared: s.Shared})
}

func (r Repository) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return r.q(ctx).DeleteService(ctx, sqlcgen.DeleteServiceParams{UserID: userID, ID: id})
}

func (r Repository) SaveMembers(ctx context.Context, userID uuid.UUID, s domain.Service) error {
	ids := make([]uuid.UUID, 0, len(s.Members))
	for _, m := range s.Members {
		ids = append(ids, m.ID)
	}
	q := r.q(ctx)
	if err := q.DeleteRemovedMembers(ctx, sqlcgen.DeleteRemovedMembersParams{UserID: userID, ServiceID: s.ID, MemberIds: ids}); err != nil {
		return err
	}
	for i, m := range s.Members {
		since, _ := time.Parse("2006-01", m.Since)
		n, err := q.UpsertMember(ctx, sqlcgen.UpsertMemberParams{ID: m.ID, UserID: userID, ServiceID: s.ID, Name: m.Name, Amount: m.Amount.Decimal(), SinceMonth: since, SortOrder: int32(i)})
		if err != nil {
			return err
		}
		if n == 0 {
			return domain.ErrIDConflict
		}
	}
	return nil
}

func (r Repository) AddPayment(ctx context.Context, userID, id uuid.UUID, p domain.Payment) error {
	month, _ := time.Parse("2006-01", p.Month)
	return r.q(ctx).InsertMemberPayment(ctx, sqlcgen.InsertMemberPaymentParams{ID: p.ID, UserID: userID, ServiceID: id, MemberID: p.MemberID, Month: month, PaidOn: p.PaidOn.Time(), Amount: p.Amount.Decimal()})
}

func (r Repository) RemovePayment(ctx context.Context, userID, id, memberID uuid.UUID, month string) error {
	t, _ := time.Parse("2006-01", month)
	return r.q(ctx).DeleteMemberPayment(ctx, sqlcgen.DeleteMemberPaymentParams{UserID: userID, ServiceID: id, MemberID: memberID, Month: t})
}

func optionalDate(d *dates.LocalDate) *time.Time {
	if d == nil {
		return nil
	}
	t := d.Time()
	return &t
}
