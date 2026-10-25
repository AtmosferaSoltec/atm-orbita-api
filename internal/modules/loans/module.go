// Package loans exposes web debtor ledgers, manual monthly plans and read-only sharing.
package loans

import (
	loanhttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
	"github.com/gin-gonic/gin"
)

type Module struct{ handler *loanhttp.Handler }

func New(runner tx.Runner, c clock.Clock, profiles profile.Reader) *Module {
	s := app.New(app.Deps{Repo: postgres.New(runner), Tx: runner, Clock: c, Profile: profiles})
	return &Module{handler: loanhttp.New(s)}
}
func (m *Module) Routes(public, protected gin.IRoutes) { m.handler.Routes(public, protected) }
