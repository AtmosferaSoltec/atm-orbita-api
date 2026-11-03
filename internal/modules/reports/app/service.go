// Package app es el caso de uso de los reportes y los puertos que necesita.
package app

import (
	"context"
	"errors"

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

// PDF dibuja el resumen como PDF (atm-documents-api). El módulo no tiene lógica de
// PDF: solo entrega las cifras.
type PDF interface {
	// Enabled dice si hay con qué generarlo (credenciales de servicio).
	Enabled() bool
	// Render devuelve ErrPDFDisabled o ErrPDFFailed si no pudo.
	Render(ctx context.Context, summary Summary) ([]byte, error)
}

// Errores del PDF.
var (
	// ErrPDFDisabled: no hay credenciales del servicio de documentos.
	ErrPDFDisabled = errors.New("pdf generation is not configured")
	// ErrPDFFailed: el servicio de documentos no respondió o rechazó la petición.
	ErrPDFFailed = errors.New("pdf generation failed")
	// ErrNothingToReport: el periodo no tiene movimientos, no hay qué imprimir.
	ErrNothingToReport = errors.New("period without movements")
)

// MaxPDFDays es el periodo más largo que acepta el servicio de documentos
// (fechas inclusive).
const MaxPDFDays = 366

// Service es el caso de uso de los reportes.
type Service struct {
	totals  Totals
	profile Profile
	pdf     PDF
}

// NewService crea el servicio. pdf puede ser nil: el PDF queda apagado.
func NewService(totals Totals, profile Profile, pdf PDF) *Service {
	return &Service{totals: totals, profile: profile, pdf: pdf}
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

// PreparePDF arma el resumen del PDF con el mismo caso de uso de Summary (mismas
// cifras, mismo recorte de `to` a hoy, mismo RANGE_INVALID) y le suma lo propio
// del PDF: que esté habilitado, que el periodo no pase de MaxPDFDays y que haya
// movimientos. Hasta aquí no se ha gastado cupo ni llamado a nadie.
func (s *Service) PreparePDF(ctx context.Context, userID uuid.UUID, from, to string) (Summary, error) {
	if s.pdf == nil || !s.pdf.Enabled() {
		return Summary{}, ErrPDFDisabled
	}
	summary, err := s.Summary(ctx, userID, from, to)
	if err != nil {
		return Summary{}, err
	}
	if days := int(summary.To.Time().Sub(summary.From.Time()).Hours()/24) + 1; days > MaxPDFDays {
		return Summary{}, &domain.ValidationError{Fields: []domain.FieldError{{Field: "to", Code: domain.CodeRangeInvalid}}}
	}
	if len(summary.Currencies) == 0 {
		return Summary{}, ErrNothingToReport
	}
	return summary, nil
}

// RenderPDF genera el PDF de un resumen ya preparado. No guarda nada.
func (s *Service) RenderPDF(ctx context.Context, summary Summary) ([]byte, error) {
	if s.pdf == nil {
		return nil, ErrPDFDisabled
	}
	return s.pdf.Render(ctx, summary)
}
