// Package app implements service CRUD and atomic member payment recording.
package app

import (
	"context"
	"errors"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
)

type Repository interface {
	List(context.Context, uuid.UUID) ([]domain.Service, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.Service, error)
	Lock(context.Context, uuid.UUID, uuid.UUID) error
	Insert(context.Context, uuid.UUID, domain.Service) (bool, error)
	Update(context.Context, uuid.UUID, domain.Service) error
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	SaveMembers(context.Context, uuid.UUID, domain.Service) error
	AddPayment(context.Context, uuid.UUID, uuid.UUID, domain.Payment) error
	RemovePayment(context.Context, uuid.UUID, uuid.UUID, uuid.UUID, string) error
}
type Transactor interface {
	InTx(context.Context, func(context.Context) error) error
}
type Profile interface {
	Today(context.Context, uuid.UUID) (dates.LocalDate, error)
}
type Deps struct {
	Repo    Repository
	Tx      Transactor
	Profile Profile
	Clock   clock.Clock
}
type Service struct{ deps Deps }

func New(deps Deps) *Service { return &Service{deps} }
func (s *Service) List(ctx context.Context, userID uuid.UUID) ([]domain.Service, dates.LocalDate, error) {
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return nil, today, err
	}
	items, err := s.deps.Repo.List(ctx, userID)
	return items, today, err
}

func (s *Service) Get(ctx context.Context, userID, id uuid.UUID) (domain.Service, error) {
	return s.deps.Repo.Get(ctx, userID, id)
}

func (s *Service) Create(ctx context.Context, userID uuid.UUID, in domain.Service) (domain.Service, bool, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return domain.Service{}, false, err
	}
	if in.ID == uuid.Nil {
		in.ID = ids.New()
	}
	in.CreatedAt = s.deps.Clock.Now()
	var result domain.Service
	var created bool
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.deps.Repo.Insert(ctx, userID, in)
		if err != nil {
			return err
		}
		if created {
			if err = s.deps.Repo.SaveMembers(ctx, userID, in); err != nil {
				return err
			}
		}
		result, err = s.deps.Repo.Get(ctx, userID, in.ID)
		if !created && errors.Is(err, domain.ErrNotFound) {
			return domain.ErrIDConflict
		}
		return err
	})
	return result, created, err
}

func (s *Service) Update(ctx context.Context, userID, id uuid.UUID, in domain.Service) (domain.Service, error) {
	in.ID = id
	in.Normalize()
	if err := in.Validate(); err != nil {
		return domain.Service{}, err
	}
	var result domain.Service
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, userID, id); err != nil {
			return err
		}
		old, err := s.deps.Repo.Get(ctx, userID, id)
		if err != nil {
			return err
		}
		if len(old.Payments) > 0 && old.Currency != in.Currency {
			return domain.ErrCurrencyLocked
		}
		if err = s.deps.Repo.Update(ctx, userID, in); err != nil {
			return err
		}
		if err = s.deps.Repo.SaveMembers(ctx, userID, in); err != nil {
			return err
		}
		result, err = s.deps.Repo.Get(ctx, userID, id)
		return err
	})
	return result, err
}

func (s *Service) Delete(ctx context.Context, userID, id uuid.UUID) error {
	return s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, userID, id); err != nil {
			return err
		}
		return s.deps.Repo.Delete(ctx, userID, id)
	})
}

func (s *Service) MarkPaid(ctx context.Context, userID, id uuid.UUID, payments []domain.Payment) (domain.Service, error) {
	if len(payments) == 0 || len(payments) > 1200 {
		return domain.Service{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "payments", Code: "SERVICE_PAYMENTS_REQUIRED"}}}
	}
	today, err := s.deps.Profile.Today(ctx, userID)
	if err != nil {
		return domain.Service{}, err
	}
	var result domain.Service
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, userID, id); err != nil {
			return err
		}
		current, err := s.deps.Repo.Get(ctx, userID, id)
		if err != nil {
			return err
		}
		for _, p := range payments {
			amount, err := current.PaymentAmount(p.MemberID, p.Month, p.PaidOn, today)
			if err != nil {
				return err
			}
			p.ID = ids.New()
			p.Amount = amount
			if err = s.deps.Repo.AddPayment(ctx, userID, id, p); err != nil {
				return err
			}
		}
		result, err = s.deps.Repo.Get(ctx, userID, id)
		return err
	})
	return result, err
}

func (s *Service) UnmarkPaid(ctx context.Context, userID, id, memberID uuid.UUID, month string) (domain.Service, error) {
	if !domain.ValidMonth(month) {
		return domain.Service{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "month", Code: "SERVICE_PAYMENT_MONTH_INVALID"}}}
	}
	var result domain.Service
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, userID, id); err != nil {
			return err
		}
		if err := s.deps.Repo.RemovePayment(ctx, userID, id, memberID, month); err != nil {
			return err
		}
		var err error
		result, err = s.deps.Repo.Get(ctx, userID, id)
		return err
	})
	return result, err
}
