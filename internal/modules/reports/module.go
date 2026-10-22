// Package reports son los reportes de un periodo: ingresos, gastos y balance por
// moneda, con el ranking por categoría. No tiene tablas: las sumas se las pide a
// ledger (ReportQuery) y la moneda principal y "hoy", a profile.
//
// Es la API pública del módulo: main solo usa lo de aquí.
package reports

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	reportshttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// Module arma el módulo.
type Module struct {
	service *app.Service
	handler *reportshttp.Handler
}

// New crea el módulo.
func New(query ledger.ReportQuery, profiles profile.Reader) *Module {
	service := app.NewService(totals{query}, profileSource{profiles})
	return &Module{service: service, handler: reportshttp.NewHandler(service)}
}

// Routes monta las rutas del módulo; piden sesión.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }

// totals adapta ledger.ReportQuery al puerto app.Totals.
type totals struct{ query ledger.ReportQuery }

func (t totals) TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]domain.Row, error) {
	rows, err := t.query.TotalsByCategory(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]domain.Row, 0, len(rows))
	for _, r := range rows {
		out = append(out, domain.Row{
			Currency: r.Currency,
			Category: domain.Category{ID: r.CategoryID, Name: r.Name, Kind: r.Kind, Color: r.Color},
			Total:    r.Total,
		})
	}
	return out, nil
}

// profileSource adapta profile.Reader al puerto app.Profile.
type profileSource struct{ profiles profile.Reader }

func (p profileSource) MainCurrency(ctx context.Context, userID uuid.UUID) (string, error) {
	settings, err := p.profiles.Settings(ctx, userID)
	return settings.Main, err
}

func (p profileSource) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return p.profiles.Today(ctx, userID)
}
