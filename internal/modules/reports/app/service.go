// Package app es el caso de uso de los reportes y los puertos que necesita.
package app

import (
	"context"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// Totals da las sumas de los movimientos de un periodo (módulo ledger).
type Totals interface {
	TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]domain.Row, error)
}

// Profile da la moneda principal y "hoy" del usuario (módulo profile).
type Profile interface {
	MainCurrency(ctx context.Context, userID uuid.UUID) (string, error)
	Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
}

// Service es el caso de uso de los reportes.
type Service struct {
	totals  Totals
	profile Profile
}

// NewService crea el servicio.
func NewService(totals Totals, profile Profile) *Service {
	return &Service{totals: totals, profile: profile}
}

// Summary es el reporte de un periodo: un bloque por moneda.
type Summary struct {
	From, To   dates.LocalDate
	Currencies []domain.CurrencyReport
}

// Summary arma el reporte del periodo. to se recorta a hoy; un periodo que
// empieza en el futuro no tiene movimientos.
func (s *Service) Summary(ctx context.Context, userID uuid.UUID, from, to string) (Summary, error) {
	today, err := s.profile.Today(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	start, end, err := domain.Period(from, to, today)
	if err != nil {
		return Summary{}, err
	}
	out := Summary{From: start, To: end, Currencies: []domain.CurrencyReport{}}
	if start.After(end) {
		return out, nil
	}
	rows, err := s.totals.TotalsByCategory(ctx, userID, start, end)
	if err != nil {
		return Summary{}, err
	}
	main, err := s.profile.MainCurrency(ctx, userID)
	if err != nil {
		return Summary{}, err
	}
	out.Currencies = domain.Build(rows, main)
	return out, nil
}
