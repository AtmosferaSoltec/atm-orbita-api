// Package home es Inicio en una sola petición. No tiene tablas ni reglas
// propias: compone las interfaces públicas de profile, ledger, reports y credit.
//
// Es la API pública del módulo: main solo usa lo de aquí.
package home

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit"
	homehttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/home/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/home/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// Module arma el módulo.
type Module struct {
	handler *homehttp.Handler
}

// New crea el módulo con lo que expone cada módulo del que lee.
func New(profiles profile.Reader, book ledger.HomeReader, totals reports.Reader, cards credit.CreditSummary) *Module {
	service := app.NewService(profileSource{profiles}, book, reportsSource{totals}, creditSource{cards})
	return &Module{handler: homehttp.NewHandler(service)}
}

// Routes monta la ruta del módulo; pide sesión.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }

type profileSource struct{ profiles profile.Reader }

func (p profileSource) Settings(ctx context.Context, userID uuid.UUID) (app.Settings, error) {
	s, err := p.profiles.Settings(ctx, userID)
	if err != nil {
		return app.Settings{}, err
	}
	return app.Settings{Main: s.Main, Secondary: s.Secondary, Display: s.DisplayCurrency, FxMode: s.FxMode, Rate: s.Rate}, nil
}

func (p profileSource) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return p.profiles.Today(ctx, userID)
}

type reportsSource struct{ totals reports.Reader }

func (r reportsSource) Totals(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]app.CurrencyTotals, error) {
	rows, err := r.totals.Totals(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]app.CurrencyTotals, 0, len(rows))
	for _, t := range rows {
		out = append(out, app.CurrencyTotals{Currency: t.Currency, Income: t.Income, Expense: t.Expense})
	}
	return out, nil
}

type creditSource struct{ cards credit.CreditSummary }

func (c creditSource) Summary(ctx context.Context, userID uuid.UUID) (app.Credit, bool, error) {
	s, ok, err := c.cards.Summary(ctx, userID)
	return app.Credit{PendingCount: s.PendingCount, NextDueDate: s.NextDueDate, HasUrgent: s.HasUrgent}, ok, err
}
