// Package documents arma lo que atm-documents-api pide para el PDF del reporte a
// partir del mismo resumen de GET /reports/summary. El módulo no dibuja nada.
package documents

import (
	"context"
	"errors"

	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/documents"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
)

// Límites de documents-api (README, period-report).
const (
	maxCurrencies = 12
	maxRows       = 50
	maxNameRunes  = 80
	othersName    = "Otros"
)

// Sender es lo que se usa del cliente de documents-api.
type Sender interface {
	Configured() bool
	PeriodReportPDF(ctx context.Context, report documents.PeriodReport) ([]byte, error)
}

// PeriodReport implementa app.PDF.
type PeriodReport struct{ sender Sender }

// NewPeriodReport crea el adaptador.
func NewPeriodReport(sender Sender) PeriodReport { return PeriodReport{sender: sender} }

// Enabled dice si hay con qué pedir el PDF.
func (p PeriodReport) Enabled() bool { return p.sender != nil && p.sender.Configured() }

// Render pide el PDF del resumen.
func (p PeriodReport) Render(ctx context.Context, summary app.Summary) ([]byte, error) {
	pdf, err := p.sender.PeriodReportPDF(ctx, Request(summary))
	switch {
	case errors.Is(err, documents.ErrNotConfigured):
		return nil, app.ErrPDFDisabled
	case errors.Is(err, documents.ErrUnavailable):
		return nil, errors.Join(app.ErrPDFFailed, err)
	}
	return pdf, err
}

// Request convierte el resumen al cuerpo de documents-api: dinero con 2
// decimales, porcentaje con 1, y los límites de filas y de texto.
func Request(summary app.Summary) documents.PeriodReport {
	out := documents.PeriodReport{From: summary.From.String(), To: summary.To.String(), Currencies: []documents.Currency{}}
	for i, c := range summary.Currencies {
		if i == maxCurrencies {
			break
		}
		out.Currencies = append(out.Currencies, documents.Currency{
			Currency:     c.Currency,
			IncomeTotal:  c.IncomeTotal.StringFixed(2),
			ExpenseTotal: c.ExpenseTotal.StringFixed(2),
			Balance:      c.Balance.StringFixed(2),
			Expenses:     lines(c.Expenses, c.ExpenseTotal),
			Incomes:      lines(c.Incomes, c.IncomeTotal),
		})
	}
	return out
}

// lines pasa un ranking. Si pasa del máximo, las últimas filas se juntan en
// "Otros" para que el total y los porcentajes sigan cuadrando con el resumen.
func lines(in []domain.Line, whole decimal.Decimal) []documents.Line {
	out := make([]documents.Line, 0, min(len(in), maxRows))
	for i, l := range in {
		if len(in) > maxRows && i == maxRows-1 {
			rest := decimal.Zero
			for _, r := range in[i:] {
				rest = rest.Add(r.Total)
			}
			out = append(out, documents.Line{Name: othersName, Total: rest.StringFixed(2), Percent: domain.Percent(rest, whole).StringFixed(1)})
			break
		}
		out = append(out, documents.Line{Name: clip(l.Category.Name), Total: l.Total.StringFixed(2), Percent: l.Percent.StringFixed(1)})
	}
	return out
}

// clip acota el nombre a lo que acepta documents-api; uno vacío no existe en
// categorías, pero nunca debe romper el PDF.
func clip(name string) string {
	runes := []rune(name)
	if len(runes) == 0 {
		return "-"
	}
	if len(runes) > maxNameRunes {
		return string(runes[:maxNameRunes-1]) + "…"
	}
	return name
}
