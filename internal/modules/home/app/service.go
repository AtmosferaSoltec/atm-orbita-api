// Package app es el caso de uso de Inicio: compone lo que dan los demás módulos
// y no tiene reglas propias.
package app

import (
	"context"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"golang.org/x/sync/errgroup"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// RecentEntries son las filas de "Últimos movimientos" (RECENT_ENTRIES en Android).
const RecentEntries = 4

// Settings son las monedas y el tipo de cambio del usuario.
type Settings struct {
	Main      string
	Secondary string
	Display   string
	FxMode    string
	Rate      *decimal.Decimal
}

// MonthTotals son los totales del mes en curso en la moneda principal.
type MonthTotals struct {
	From, To dates.LocalDate
	Currency string
	Income   decimal.Decimal
	Expense  decimal.Decimal
}

// CurrencyTotals son ingresos y gastos de un periodo en una moneda (reports).
type CurrencyTotals struct {
	Currency string
	Income   decimal.Decimal
	Expense  decimal.Decimal
}

// Credit es el aviso de tarjetas.
type Credit struct {
	PendingCount int
	NextDueDate  dates.LocalDate
	HasUrgent    bool
}

// Profile da las monedas y "hoy" (módulo profile).
type Profile interface {
	Settings(ctx context.Context, userID uuid.UUID) (Settings, error)
	Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
}

// Ledger da el ahorro y los últimos movimientos, ya con su forma JSON (módulo ledger).
type Ledger interface {
	Savings(ctx context.Context, userID uuid.UUID) (any, error)
	RecentEntries(ctx context.Context, userID uuid.UUID, limit int) ([]any, error)
}

// Reports da los totales de un periodo (módulo reports).
type Reports interface {
	Totals(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]CurrencyTotals, error)
}

// CreditSummary da el aviso de tarjetas; ok es false sin compras pendientes (módulo credit).
type CreditSummary interface {
	Summary(ctx context.Context, userID uuid.UUID) (Credit, bool, error)
}

// Service es el caso de uso de Inicio.
type Service struct {
	profile Profile
	ledger  Ledger
	reports Reports
	credit  CreditSummary
}

// NewService crea el servicio.
func NewService(profile Profile, ledger Ledger, reports Reports, credit CreditSummary) *Service {
	return &Service{profile: profile, ledger: ledger, reports: reports, credit: credit}
}

// Home es todo lo que muestra Inicio.
type Home struct {
	Today    dates.LocalDate
	Settings Settings
	Savings  any
	Month    MonthTotals
	Recent   []any
	// Credit es nil si no hay compras pendientes.
	Credit *Credit
}

// Home lee todo Inicio. "Hoy" y los ajustes van primero (el mes depende de hoy);
// lo demás va en paralelo, y si una lectura falla, falla todo: nunca un Inicio a medias.
func (s *Service) Home(ctx context.Context, userID uuid.UUID) (Home, error) {
	var out Home
	first, firstCtx := errgroup.WithContext(ctx)
	first.Go(func() (err error) {
		out.Today, err = s.profile.Today(firstCtx, userID)
		return err
	})
	first.Go(func() (err error) {
		out.Settings, err = s.profile.Settings(firstCtx, userID)
		return err
	})
	if err := first.Wait(); err != nil {
		return Home{}, err
	}

	out.Month = MonthTotals{
		From:     dates.New(out.Today.Time().Year(), out.Today.Time().Month(), 1),
		To:       out.Today,
		Currency: out.Settings.Main,
	}
	group, groupCtx := errgroup.WithContext(ctx)
	group.Go(func() (err error) {
		out.Savings, err = s.ledger.Savings(groupCtx, userID)
		return err
	})
	group.Go(func() (err error) {
		out.Recent, err = s.ledger.RecentEntries(groupCtx, userID, RecentEntries)
		return err
	})
	group.Go(func() error {
		totals, err := s.reports.Totals(groupCtx, userID, out.Month.From, out.Month.To)
		for _, t := range totals {
			if t.Currency == out.Settings.Main {
				out.Month.Income, out.Month.Expense = t.Income, t.Expense
			}
		}
		return err
	})
	group.Go(func() error {
		credit, ok, err := s.credit.Summary(groupCtx, userID)
		if ok {
			out.Credit = &credit
		}
		return err
	})
	if err := group.Wait(); err != nil {
		return Home{}, err
	}
	return out, nil
}
