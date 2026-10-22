// Package ledger es el libro del usuario: categorías, cuentas, movimientos y
// transferencias.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package ledger

import (
	"context"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"

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
	transfersHandler    *ledgerhttp.Transfers
	transactionRepo     postgres.Transactions
	categoryRepo        postgres.Categories
}

// New crea el módulo. Del perfil lee las monedas, el tipo de cambio y "hoy".
func New(runner tx.Runner, c clock.Clock, profiles profile.Reader) *Module {
	categoryRepo := postgres.NewCategories(runner)
	accountRepo := postgres.NewAccounts(runner)
	source := profileSource{profiles}
	categories := app.NewCategoryService(categoryRepo, c)
	accounts := app.NewAccountService(accountRepo, source, c)
	transactionRepo := postgres.NewTransactions(runner)
	transactions := app.NewTransactionService(transactionRepo, accountRepo, categoryRepo, source, c)
	transfers := app.NewTransferService(postgres.NewTransfers(runner), accountRepo, source, c)
	return &Module{
		categories:          categories,
		accounts:            accounts,
		categoriesHandler:   ledgerhttp.NewCategories(categories),
		accountsHandler:     ledgerhttp.NewAccounts(accounts),
		transactionsHandler: ledgerhttp.NewTransactions(transactions),
		transactionRepo:     transactionRepo,
		categoryRepo:        categoryRepo,
		transfersHandler:    ledgerhttp.NewTransfers(transfers),
	}
}

// Routes monta las rutas del módulo; todas piden sesión.
func (m *Module) Routes(protected gin.IRoutes) {
	m.categoriesHandler.Routes(protected)
	m.accountsHandler.Routes(protected)
	m.transactionsHandler.Routes(protected)
	m.transfersHandler.Routes(protected)
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

// ErrNotFound: lo pedido no existe o es de otro usuario.
var ErrNotFound = domain.ErrNotFound

// CategoryInfo es una categoría vista desde otro módulo.
type CategoryInfo struct {
	ID       uuid.UUID
	Name     string
	Kind     string
	Color    string
	Archived bool
}

// CategoryReader es lo que credit lee de las categorías: las del usuario por su
// id, también las archivadas (cada módulo decide si las acepta).
type CategoryReader interface {
	Categories(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]CategoryInfo, error)
}

// CategoryReader da acceso de lectura a las categorías.
func (m *Module) CategoryReader() CategoryReader { return categoryReader{m.categoryRepo} }

type categoryReader struct{ repo postgres.Categories }

func (r categoryReader) Categories(ctx context.Context, userID uuid.UUID, ids []uuid.UUID) (map[uuid.UUID]CategoryInfo, error) {
	rows, err := r.repo.Find(ctx, userID, ids)
	if err != nil {
		return nil, err
	}
	out := make(map[uuid.UUID]CategoryInfo, len(rows))
	for _, c := range rows {
		out[c.ID] = CategoryInfo{ID: c.ID, Name: c.Name, Kind: string(c.Kind), Color: c.Color, Archived: c.Archived}
	}
	return out, nil
}

// CategoryTotal es la suma de los movimientos de una categoría en un periodo, en
// la moneda de sus cuentas (sin convertir).
type CategoryTotal struct {
	Currency   string
	Kind       string
	CategoryID uuid.UUID
	Name       string
	Color      string
	Total      decimal.Decimal
	Movements  int64
}

// ReportQuery es lo que el módulo reports pide al libro: solo movimientos (no
// transferencias), sin los eliminados.
type ReportQuery interface {
	TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]CategoryTotal, error)
}

// ReportQuery da las sumas para los reportes.
func (m *Module) ReportQuery() ReportQuery { return reportQuery{m.transactionRepo} }

type reportQuery struct{ repo postgres.Transactions }

func (q reportQuery) TotalsByCategory(ctx context.Context, userID uuid.UUID, from, to dates.LocalDate) ([]CategoryTotal, error) {
	rows, err := q.repo.TotalsByCategory(ctx, userID, from, to)
	if err != nil {
		return nil, err
	}
	out := make([]CategoryTotal, 0, len(rows))
	for _, r := range rows {
		out = append(out, CategoryTotal{
			Currency: r.Currency, Kind: string(r.Kind), CategoryID: r.Category.ID, Name: r.Category.Name,
			Color: r.Category.Color, Total: r.Total, Movements: r.Movements,
		})
	}
	return out, nil
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
