// Package app son los casos de uso del perfil y los puertos que necesitan.
package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Origen de un tipo de cambio guardado.
const sourceManual = "manual"

// StoredProfile es una fila de profiles.
type StoredProfile struct {
	Main, Secondary, Display, Timezone, FxMode string
}

// StoredRate es el tipo de cambio más reciente de un par, en el sentido guardado.
type StoredRate struct {
	From, To  string
	Rate      decimal.Decimal
	CreatedAt time.Time
}

// NewRate es una fila nueva de exchange_rates.
type NewRate struct {
	ID       uuid.UUID
	UserID   uuid.UUID
	From, To string
	Rate     decimal.Decimal
	Source   string
}

// Profiles guarda perfiles y tipos de cambio.
type Profiles interface {
	Create(ctx context.Context, userID uuid.UUID, p StoredProfile) error
	// Get y Lock devuelven domain.ErrNotFound si no hay perfil.
	Get(ctx context.Context, userID uuid.UUID) (StoredProfile, error)
	Lock(ctx context.Context, userID uuid.UUID) (StoredProfile, error)
	Update(ctx context.Context, userID uuid.UUID, p StoredProfile) error
	InsertRate(ctx context.Context, rate NewRate) error
	// LatestRate busca el par en cualquiera de los dos sentidos; ok es false si no hay.
	LatestRate(ctx context.Context, userID uuid.UUID, a, b string) (rate StoredRate, ok bool, err error)
}

// Transactor abre una transacción (platform/tx.Runner).
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// Settings es lo que ve la pantalla Ajustes.
type Settings struct {
	domain.Profile
	RateUpdatedAt *time.Time
}

// Service son los casos de uso del perfil.
type Service struct {
	profiles Profiles
	tx       Transactor
	clock    clock.Clock
}

// NewService crea el servicio.
func NewService(profiles Profiles, tx Transactor, c clock.Clock) *Service {
	return &Service{profiles: profiles, tx: tx, clock: c}
}

// CreateForNewUser es el gancho de alta: perfil con la moneda elegida, la
// secundaria por defecto, la vista en la principal y sin tipo de cambio.
func (s *Service) CreateForNewUser(ctx context.Context, userID uuid.UUID, mainCurrency, timezone string) error {
	return s.profiles.Create(ctx, userID, StoredProfile{
		Main:      mainCurrency,
		Secondary: domain.SecondaryFor(mainCurrency),
		Display:   mainCurrency,
		Timezone:  timezone,
		FxMode:    domain.FxModeManual,
	})
}

// Get devuelve los ajustes con el tipo de cambio vigente (o sin él).
func (s *Service) Get(ctx context.Context, userID uuid.UUID) (Settings, error) {
	stored, err := s.profiles.Get(ctx, userID)
	if err != nil {
		return Settings{}, err
	}
	return s.withRate(ctx, userID, stored)
}

func (s *Service) withRate(ctx context.Context, userID uuid.UUID, stored StoredProfile) (Settings, error) {
	settings := Settings{Profile: domain.Profile{
		FxPair:          domain.FxPair{Main: stored.Main, Secondary: stored.Secondary},
		DisplayCurrency: stored.Display,
		Timezone:        stored.Timezone,
		FxMode:          stored.FxMode,
	}}
	latest, ok, err := s.profiles.LatestRate(ctx, userID, stored.Main, stored.Secondary)
	if err != nil {
		return Settings{}, fmt.Errorf("latest rate: %w", err)
	}
	if !ok {
		return settings, nil
	}
	// Guardado como Secondary → Main: es el rate tal cual. Al revés: su inverso.
	rate := latest.Rate
	if latest.From == stored.Main {
		rate = domain.Inverse(latest.Rate)
	}
	settings.Rate = &rate
	settings.RateUpdatedAt = &latest.CreatedAt
	return settings, nil
}

// SetFxInput es el par de monedas y su tipo de cambio, que se guardan juntos.
type SetFxInput struct {
	Main      string
	Secondary string
	Rate      *string
}

// SetFx guarda el par y su tipo de cambio, nunca uno sin el otro: un par nuevo
// sin un valor mayor que cero no se guarda (la app no inventa un 1.00).
func (s *Service) SetFx(ctx context.Context, userID uuid.UUID, in SetFxInput) (Settings, error) {
	var problems domain.ValidationError
	if !currency.IsSupported(in.Main) {
		problems.Add("main_currency", domain.CodeCurrencyNotSupported)
	}
	if !currency.IsSupported(in.Secondary) {
		problems.Add("secondary_currency", domain.CodeCurrencyNotSupported)
	}
	if in.Main == in.Secondary {
		problems.Add("secondary_currency", domain.CodeSameCurrency)
	}
	rate, rateErr := parsePositiveRate(in.Rate)
	if rateErr != "" {
		problems.Add("rate", rateErr)
	}
	if err := problems.OrNil(); err != nil {
		return Settings{}, err
	}

	var settings Settings
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := s.profiles.Lock(ctx, userID)
		if err != nil {
			return err
		}
		current.Display = domain.DisplayAfterPairChange(current.Display, in.Main, in.Secondary)
		current.Main, current.Secondary = in.Main, in.Secondary
		if err := s.profiles.Update(ctx, userID, current); err != nil {
			return err
		}
		if err := s.profiles.InsertRate(ctx, NewRate{
			ID: ids.New(), UserID: userID, From: in.Secondary, To: in.Main, Rate: rate, Source: sourceManual,
		}); err != nil {
			return err
		}
		settings, err = s.withRate(ctx, userID, current)
		return err
	})
	return settings, err
}

// UpdateInput son los cambios sueltos de Ajustes; nil = sin cambio.
type UpdateInput struct {
	DisplayCurrency *string
	Timezone        *string
}

// Update cambia la moneda de visualización o la zona horaria.
func (s *Service) Update(ctx context.Context, userID uuid.UUID, in UpdateInput) (Settings, error) {
	var settings Settings
	err := s.tx.InTx(ctx, func(ctx context.Context) error {
		current, err := s.profiles.Lock(ctx, userID)
		if err != nil {
			return err
		}
		withRate, err := s.withRate(ctx, userID, current)
		if err != nil {
			return err
		}

		var problems domain.ValidationError
		if in.DisplayCurrency != nil && *in.DisplayCurrency != current.Main && *in.DisplayCurrency != current.Secondary {
			problems.Add("display_currency", domain.CodeCurrencyNotInPair)
		}
		if in.Timezone != nil {
			if _, err := dates.LoadLocation(*in.Timezone); err != nil {
				problems.Add("timezone", domain.CodeTimezoneInvalid)
			}
		}
		if err := problems.OrNil(); err != nil {
			return err
		}
		if in.DisplayCurrency != nil {
			// Sin tipo de cambio no se ve el ahorro en la secundaria.
			if err := withRate.UsableCurrency(*in.DisplayCurrency); err != nil {
				return err
			}
			current.Display = *in.DisplayCurrency
		}
		if in.Timezone != nil {
			current.Timezone = *in.Timezone
		}
		if err := s.profiles.Update(ctx, userID, current); err != nil {
			return err
		}
		withRate.DisplayCurrency, withRate.Timezone = current.Display, current.Timezone
		settings = withRate
		return nil
	})
	return settings, err
}

// RequireUsable devuelve domain.ErrFxNotConfigured si currency no se puede usar todavía.
func (s *Service) RequireUsable(ctx context.Context, userID uuid.UUID, currencyCode string) error {
	settings, err := s.Get(ctx, userID)
	if err != nil {
		return err
	}
	return settings.UsableCurrency(currencyCode)
}

// Today es el día de hoy en la zona horaria del perfil.
func (s *Service) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	stored, err := s.profiles.Get(ctx, userID)
	if err != nil {
		return dates.LocalDate{}, err
	}
	location, err := dates.LoadLocation(stored.Timezone)
	if err != nil {
		return dates.LocalDate{}, fmt.Errorf("profile time zone: %w", err)
	}
	return dates.Today(s.clock, location), nil
}

// parsePositiveRate devuelve el código del problema, o "" si el valor sirve.
func parsePositiveRate(text *string) (decimal.Decimal, string) {
	if text == nil || *text == "" {
		return decimal.Decimal{}, domain.CodeRateRequired
	}
	rate, err := money.ParseRate(*text)
	if errors.Is(err, money.ErrInvalidRate) {
		return decimal.Decimal{}, domain.CodeInvalidFormat
	}
	if !rate.IsPositive() {
		return decimal.Decimal{}, domain.CodeRateRequired
	}
	return rate.Decimal(), ""
}
