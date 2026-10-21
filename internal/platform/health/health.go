// Package health expone las rutas de salud.
package health

import (
	"context"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// readyTimeout: si la base no responde en este tiempo, el API no está listo.
const readyTimeout = 2 * time.Second

// Pinger comprueba que la base responde.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Register monta las rutas en el grupo /api/v1:
//
//   - GET /health: el proceso responde (liveness). La usa el healthcheck de
//     Docker: que la base falle no debe reiniciar el contenedor en bucle.
//   - GET /health/ready: además, la base responde.
func Register(routes gin.IRoutes, db Pinger) {
	routes.GET("/health", func(c *gin.Context) {
		apierror.OK(c, gin.H{"status": "ok"})
	})
	routes.GET("/health/ready", func(c *gin.Context) {
		ctx, cancel := context.WithTimeout(c.Request.Context(), readyTimeout)
		defer cancel()
		if err := db.Ping(ctx); err != nil {
			logger.From(c.Request.Context()).Warn("database not ready", "error", err)
			apierror.Respond(c, apierror.ErrDatabaseDown)
			return
		}
		apierror.OK(c, gin.H{"status": "ok", "database": "ok"})
	})
}
