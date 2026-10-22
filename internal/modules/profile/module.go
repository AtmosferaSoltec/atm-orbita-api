// Package profile son las preferencias del usuario: sus dos monedas, en cuál ve
// el ahorro, su zona horaria y el tipo de cambio manual, que puede no existir.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package profile

import (
	"context"
	"errors"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	profilehttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// ErrFxNotConfigured: algo en una moneda distinta de la principal antes de
// configurar el tipo de cambio. Los demás módulos lo traducen a 422
// FX_RATE_NOT_CONFIGURED.
var ErrFxNotConfigured = domain.ErrFxNotConfigured

// IsFxNotConfigured dice si err es ErrFxNotConfigured.
func IsFxNotConfigured(err error) bool { return errors.Is(err, ErrFxNotConfigured) }

// FxPair son las dos monedas y el tipo de cambio (nil si no existe).
type FxPair = domain.FxPair

// Settings son las preferencias de un usuario.
type Settings = app.Settings

// Reader es lo que los demás módulos pueden pedir del perfil.
type Reader interface {
	Settings(ctx context.Context, userID uuid.UUID) (Settings, error)
	// Today es el día de hoy en la zona horaria del usuario.
	Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error)
	// RequireUsable devuelve ErrFxNotConfigured si currency no se puede usar todavía.
	RequireUsable(ctx context.Context, userID uuid.UUID, currency string) error
}

// Module arma el módulo con sus adaptadores.
type Module struct {
	service *app.Service
	handler *profilehttp.Handler
}

// New crea el módulo.
func New(runner tx.Runner, c clock.Clock) *Module {
	service := app.NewService(postgres.NewProfiles(runner), runner, c)
	return &Module{service: service, handler: profilehttp.NewHandler(service)}
}

// Routes monta las rutas del módulo.
func (m *Module) Routes(public, protected gin.IRoutes) { m.handler.Routes(public, protected) }

// OnUserRegistered crea el perfil de un usuario nuevo. Corre dentro de la
// transacción del registro (identity).
func (m *Module) OnUserRegistered(ctx context.Context, userID uuid.UUID, mainCurrency, timezone string) error {
	return m.service.CreateForNewUser(ctx, userID, mainCurrency, timezone)
}

// Reader da acceso de lectura al perfil.
func (m *Module) Reader() Reader { return reader{m.service} }

type reader struct{ service *app.Service }

func (r reader) Settings(ctx context.Context, userID uuid.UUID) (Settings, error) {
	return r.service.Get(ctx, userID)
}

func (r reader) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return r.service.Today(ctx, userID)
}

func (r reader) RequireUsable(ctx context.Context, userID uuid.UUID, currency string) error {
	return r.service.RequireUsable(ctx, userID, currency)
}
