// Package ledger es el libro del usuario: categorías y cuentas y, en los planes
// que siguen, movimientos y transferencias.
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
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Module arma el módulo con sus adaptadores.
type Module struct {
	categories          *app.CategoryService
	accounts            *app.AccountService
	categoriesHandler   *ledgerhttp.Categories
	accountsHandler     *ledgerhttp.Accounts
	transactionsHandler *ledgerhttp.Transactions
}

// New crea el módulo. Del perfil lee las monedas, el tipo de cambio y "hoy".
func New(runner tx.Runner, c clock.Clock, profiles profile.Reader) *Module {
	categoryRepo := postgres.NewCategories(runner)
	accountRepo := postgres.NewAccounts(runner)
	source := profileSource{profiles}
	categories := app.NewCategoryService(categoryRepo, c)
	accounts := app.NewAccountService(accountRepo, source, c)
	transactions := app.NewTransactionService(postgres.NewTransactions(runner), accountRepo, categoryRepo, source, c)
	return &Module{
		categories:          categories,
		accounts:            accounts,
		categoriesHandler:   ledgerhttp.NewCategories(categories),
		accountsHandler:     ledgerhttp.NewAccounts(accounts),
		transactionsHandler: ledgerhttp.NewTransactions(transactions),
	}
}

// Routes monta las rutas del módulo; todas piden sesión.
func (m *Module) Routes(protected gin.IRoutes) {
	m.categoriesHandler.Routes(protected)
	m.accountsHandler.Routes(protected)
	m.transactionsHandler.Routes(protected)
}

// OnUserRegistered crea las 9 categorías iniciales y la cuenta "Efectivo" en la
// moneda principal. Corre dentro de la transacción del registro (identity),
// después del perfil.
func (m *Module) OnUserRegistered(ctx context.Context, userID uuid.UUID, mainCurrency string) error {
	if err := m.categories.CreateInitial(ctx, userID); err != nil {
		return err
	}
	return m.accounts.CreateInitial(ctx, userID, mainCurrency)
}

// profileSource adapta el perfil al puerto app.Profile.
type profileSource struct{ profiles profile.Reader }

func (s profileSource) Fx(ctx context.Context, userID uuid.UUID) (domain.Fx, error) {
	settings, err := s.profiles.Settings(ctx, userID)
	if err != nil {
		return domain.Fx{}, err
	}
	return domain.Fx{Main: settings.Main, Secondary: settings.Secondary, Rate: settings.Rate}, nil
}

func (s profileSource) Today(ctx context.Context, userID uuid.UUID) (dates.LocalDate, error) {
	return s.profiles.Today(ctx, userID)
}
