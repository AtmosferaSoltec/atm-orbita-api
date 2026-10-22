// Package credit son las tarjetas de crédito y sus compras pendientes de pago.
// Lee categorías de ledger y monedas y "hoy" de profile, por sus interfaces.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package credit

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	credithttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Module arma el módulo con sus adaptadores.
type Module struct {
	service *app.Service
	handler *credithttp.Handler
}

// New crea el módulo.
func New(runner tx.Runner, c clock.Clock, categories ledger.CategoryReader, profiles profile.Reader, payments ledger.PaymentLedger) *Module {
	service := app.NewService(postgres.NewCards(runner), postgres.NewPurchases(runner),
		categorySource{categories}, profileSource{profiles}, paymentSource{payments}, runner, c)
	return &Module{service: service, handler: credithttp.NewHandler(service)}
}

// Routes monta las rutas del módulo; todas piden sesión.
func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }

// Summary es el aviso de tarjetas de Inicio.
type Summary struct {
	PendingCount int
	NextDueDate  dates.LocalDate
	HasUrgent    bool
}

// CreditSummary es lo que Inicio (plan 08) pide de las tarjetas.
type CreditSummary interface {
	// Summary devuelve ok=false si no hay compras pendientes.
	Summary(ctx context.Context, userID uuid.UUID) (summary Summary, ok bool, err error)
}

// CreditSummary da el resumen de compras pendientes.
func (m *Module) CreditSummary() CreditSummary { return summarySource{m.service} }

type summarySource struct{ service *app.Service }

func (s summarySource) Summary(ctx context.Context, userID uuid.UUID) (Summary, bool, error) {
	summary, ok, err := s.service.Summary(ctx, userID)
	return Summary{PendingCount: summary.PendingCount, NextDueDate: summary.NextDueDate, HasUrgent: summary.HasUrgent}, ok, err
}

// categorySource adapta ledger.CategoryReader al puerto app.Categories.
type categorySource struct{ reader ledger.CategoryReader }

func (s categorySource) Find(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]app.FoundCategory, error) {
	found, err := s.reader.Categories(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]app.FoundCategory, len(found))
	for id, c := range found {
		out[id] = app.FoundCategory{
			Category: domain.Category{ID: c.ID, Name: c.Name, Kind: c.Kind, Color: c.Color},
			Archived: c.Archived,
		}
	}
	return out, nil
}

// profileSource adapta profile.Reader al puerto app.Profile.
type profileSource struct{ profiles profile.Reader }

func (p profileSource) MainCurrency(ctx context.Context, userID uuid.UUID) (string, error) {
	settings, err := p.profiles.Settings(ctx, userID)
	return settings.Main, err
}

func (p profileSource) RequireUsable(ctx context.Context, userID uuid.UUID, code string) error {
	err := p.profiles.RequireUsable(ctx, userID, code)
	if errors.Is(err, profile.ErrFxNotConfigured) {
		return domain.ErrFxNotConfigured
	}
	return err
}

func (p profileSource) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return p.profiles.Today(ctx, userID)
}

// RevertPayment vuelve a pendiente la compra cuyo egreso de pago se eliminó
// (ledger.PaymentReverter; lo cablea main). Corre en la transacción del libro.
func (m *Module) RevertPayment(ctx context.Context, userID, purchaseID uuid.UUID) error {
	return m.service.RevertPayment(ctx, userID, purchaseID)
}

// paymentSource adapta ledger.PaymentLedger al puerto app.PaymentLedger.
type paymentSource struct{ payments ledger.PaymentLedger }

func (p paymentSource) CreatePaymentExpense(ctx context.Context, userID uuid.UUID, in app.PaymentExpense) (uuid.UUID, any, error) {
	id, entry, err := p.payments.CreatePaymentExpense(ctx, userID, ledger.PaymentExpense{
		PurchaseID: in.PurchaseID, AccountID: in.AccountID, CategoryID: in.CategoryID,
		Description: in.Description, Amount: in.Amount, OccurredOn: in.OccurredOn,
	})
	switch {
	case errors.Is(err, ledger.ErrNotFound):
		return uuid.UUID{}, nil, domain.ErrNotFound
	case errors.Is(err, ledger.ErrAccountArchived):
		return uuid.UUID{}, nil, domain.ErrAccountArchived
	}
	return id, entry, err
}
