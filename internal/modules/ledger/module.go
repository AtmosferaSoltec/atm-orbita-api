// Package ledger es el libro del usuario: categorías y, en los planes que
// siguen, cuentas, movimientos y transferencias.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package ledger

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	ledgerhttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Module arma el módulo con sus adaptadores.
type Module struct {
	categories        *app.CategoryService
	categoriesHandler *ledgerhttp.Categories
}

// New crea el módulo.
func New(runner tx.Runner, c clock.Clock) *Module {
	categories := app.NewCategoryService(postgres.NewCategories(runner), c)
	return &Module{categories: categories, categoriesHandler: ledgerhttp.NewCategories(categories)}
}

// Routes monta las rutas del módulo; todas piden sesión.
func (m *Module) Routes(protected gin.IRoutes) { m.categoriesHandler.Routes(protected) }

// OnUserRegistered crea las categorías iniciales de un usuario nuevo. Corre
// dentro de la transacción del registro (identity), después del perfil.
func (m *Module) OnUserRegistered(ctx context.Context, userID uuid.UUID) error {
	return m.categories.CreateInitial(ctx, userID)
}
