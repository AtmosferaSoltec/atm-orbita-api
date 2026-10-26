// Package app coordinates owner-scoped loan mutations under debtor row locks.
package app

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/google/uuid"
)

type Repository interface {
	List(context.Context, uuid.UUID) ([]domain.Debtor, error)
	Get(context.Context, uuid.UUID, uuid.UUID) (domain.Debtor, error)
	Shared(context.Context, string) (domain.Debtor, error)
	Lock(context.Context, uuid.UUID, uuid.UUID) error
	Insert(context.Context, uuid.UUID, domain.Debtor) (bool, error)
	Rename(context.Context, uuid.UUID, uuid.UUID, string) error
	Delete(context.Context, uuid.UUID, uuid.UUID) error
	InsertMovement(context.Context, uuid.UUID, uuid.UUID, domain.Movement) (bool, error)
	UpdateMovement(context.Context, uuid.UUID, uuid.UUID, domain.Movement) error
	DeleteMovement(context.Context, uuid.UUID, uuid.UUID, uuid.UUID) error
	SavePlans(context.Context, uuid.UUID, uuid.UUID, []domain.Plan) error
	SetShare(context.Context, uuid.UUID, uuid.UUID, *string) error
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
func (s *Service) List(ctx context.Context, user uuid.UUID) ([]domain.Debtor, dates.LocalDate, error) {
	today, err := s.deps.Profile.Today(ctx, user)
	if err != nil {
		return nil, today, err
	}
	var result []domain.Debtor
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error { var err error; result, err = s.deps.Repo.List(ctx, user); return err })
	return result, today, err
}
func (s *Service) Get(ctx context.Context, user, id uuid.UUID) (domain.Debtor, error) {
	var result domain.Debtor
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.deps.Repo.Get(ctx, user, id)
		return err
	})
	return result, err
}
func (s *Service) Shared(ctx context.Context, token string) (domain.Debtor, error) {
	raw, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil || len(raw) != 32 {
		return domain.Debtor{}, domain.ErrNotFound
	}
	var result domain.Debtor
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		result, err = s.deps.Repo.Shared(ctx, token)
		return err
	})
	return result, err
}
func (s *Service) Create(ctx context.Context, user uuid.UUID, in domain.Debtor) (domain.Debtor, bool, error) {
	in.Normalize()
	if err := in.Validate(); err != nil {
		return domain.Debtor{}, false, err
	}
	if in.ID == uuid.Nil {
		in.ID = ids.New()
	}
	in.CreatedAt = s.deps.Clock.Now()
	in.ShareToken = nil
	var result domain.Debtor
	var created bool
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		var err error
		created, err = s.deps.Repo.Insert(ctx, user, in)
		if err != nil {
			return err
		}
		result, err = s.deps.Repo.Get(ctx, user, in.ID)
		if !created && (errors.Is(err, domain.ErrNotFound) || (err == nil && (result.Name != in.Name || result.Currency != in.Currency))) {
			return domain.ErrIDConflict
		}
		return err
	})
	return result, created, err
}
func (s *Service) mutate(ctx context.Context, user, id uuid.UUID, fn func(context.Context, *domain.Debtor) error) (domain.Debtor, error) {
	var result domain.Debtor
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, user, id); err != nil {
			return err
		}
		current, err := s.deps.Repo.Get(ctx, user, id)
		if err != nil {
			return err
		}
		if err = fn(ctx, &current); err != nil {
			return err
		}
		result, err = s.deps.Repo.Get(ctx, user, id)
		return err
	})
	return result, err
}
func (s *Service) Rename(ctx context.Context, user, id uuid.UUID, name, currency string) (domain.Debtor, error) {
	return s.mutate(ctx, user, id, func(ctx context.Context, d *domain.Debtor) error {
		if currency != "" && currency != d.Currency {
			return domain.ErrCurrencyLocked
		}
		d.Name = name
		d.Normalize()
		if err := d.Validate(); err != nil {
			return err
		}
		return s.deps.Repo.Rename(ctx, user, id, d.Name)
	})
}
func (s *Service) Delete(ctx context.Context, user, id uuid.UUID) error {
	return s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		if err := s.deps.Repo.Lock(ctx, user, id); err != nil {
			return err
		}
		return s.deps.Repo.Delete(ctx, user, id)
	})
}
func (s *Service) SaveMovement(ctx context.Context, user, id uuid.UUID, in domain.Movement, updating bool) (domain.Debtor, bool, error) {
	today, err := s.deps.Profile.Today(ctx, user)
	if err != nil {
		return domain.Debtor{}, false, err
	}
	in.Normalize()
	if err = in.Validate(today); err != nil {
		return domain.Debtor{}, false, err
	}
	if in.ID == uuid.Nil {
		in.ID = ids.New()
	}
	in.CreatedAt = s.deps.Clock.Now()
	created := false
	result, err := s.mutate(ctx, user, id, func(ctx context.Context, d *domain.Debtor) error {
		index := -1
		for i, m := range d.Movements {
			if m.ID == in.ID {
				index = i
				break
			}
		}
		if updating && index < 0 {
			return domain.ErrNotFound
		}
		if index >= 0 {
			if !updating {
				if !domain.SameMovement(d.Movements[index], in) {
					return domain.ErrIDConflict
				}
				return nil
			}
			if d.Movements[index].Kind != in.Kind {
				return domain.ErrKindLocked
			}
			in.CreatedAt = d.Movements[index].CreatedAt
			d.Movements[index] = in
		} else {
			d.Movements = append(d.Movements, in)
		}
		if err := domain.ValidateLedger(d.Movements); err != nil {
			return err
		}
		if index < 0 {
			inserted, err := s.deps.Repo.InsertMovement(ctx, user, id, in)
			if err != nil {
				return err
			}
			if !inserted {
				return domain.ErrIDConflict
			}
			created = true
			return nil
		}
		return s.deps.Repo.UpdateMovement(ctx, user, id, in)
	})
	return result, created, err
}
func (s *Service) RemoveMovement(ctx context.Context, user, id, movement uuid.UUID) (domain.Debtor, error) {
	return s.mutate(ctx, user, id, func(ctx context.Context, d *domain.Debtor) error {
		found := false
		remaining := make([]domain.Movement, 0, len(d.Movements))
		for _, m := range d.Movements {
			if m.ID == movement {
				found = true
			} else {
				remaining = append(remaining, m)
			}
		}
		if !found {
			return domain.ErrNotFound
		}
		if err := domain.ValidateLedger(remaining); err != nil {
			return err
		}
		return s.deps.Repo.DeleteMovement(ctx, user, id, movement)
	})
}
func (s *Service) Plan(ctx context.Context, user, id uuid.UUID, plans []domain.Plan) (domain.Debtor, error) {
	if err := domain.ValidatePlan(plans); err != nil {
		return domain.Debtor{}, err
	}
	return s.mutate(ctx, user, id, func(ctx context.Context, d *domain.Debtor) error {
		d.Plans = plans
		return s.deps.Repo.SavePlans(ctx, user, id, plans)
	})
}
func (s *Service) Share(ctx context.Context, user, id uuid.UUID, enabled bool) (domain.Debtor, error) {
	return s.mutate(ctx, user, id, func(ctx context.Context, d *domain.Debtor) error {
		var token *string
		if enabled {
			raw := make([]byte, 32)
			if _, err := rand.Read(raw); err != nil {
				return err
			}
			encoded := base64.RawURLEncoding.EncodeToString(raw)
			token = &encoded
		}
		return s.deps.Repo.SetShare(ctx, user, id, token)
	})
}
