// Package services exposes monthly subscriptions for the web, with manual member shares.
package services

import (
	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	servicehttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

type Module struct{ handler *servicehttp.Handler }

func New(runner tx.Runner, c clock.Clock, profiles profile.Reader) *Module {
	service := app.New(app.Deps{Repo: postgres.New(runner), Tx: runner, Clock: c, Profile: profiles})
	return &Module{handler: servicehttp.New(service)}
}

func (m *Module) Routes(protected gin.IRoutes) { m.handler.Routes(protected) }
