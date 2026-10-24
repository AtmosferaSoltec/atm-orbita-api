// Package httpserver arma Gin con los middlewares comunes y corre el servidor
// HTTP con tiempos máximos y cierre ordenado.
package httpserver

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/middleware"
)

// APIPrefix es el prefijo de todas las rutas.
const APIPrefix = "/api/v1"

// maxBodyBytes: ningún cuerpo del API se acerca a esto; más es un abuso.
const maxBodyBytes = 1 << 20

// Solo se cree a X-Forwarded-For si la petición llega desde aquí: el nginx del
// host y la red de Docker. Desde internet nadie puede falsear su IP.
var trustedProxies = []string{"127.0.0.1", "::1", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "fc00::/7"}

// Options es lo que necesita el router.
type Options struct {
	Production         bool
	Logger             *slog.Logger
	CORSOrigins        []string
	RateLimitPerMinute int
	// LargeBodyRoutes son las rutas (relativas a /api/v1, como las monta el
	// módulo) que aceptan un cuerpo mayor al general, con su límite.
	LargeBodyRoutes map[string]int64
}

// NewRouter devuelve Gin con los middlewares comunes y el grupo /api/v1, donde
// cada módulo monta sus rutas.
func NewRouter(opts Options) (*gin.Engine, *gin.RouterGroup, error) {
	if opts.Production {
		gin.SetMode(gin.ReleaseMode)
	}
	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.RemoteIPHeaders = []string{"X-Forwarded-For"}
	if err := router.SetTrustedProxies(trustedProxies); err != nil {
		return nil, nil, fmt.Errorf("set trusted proxies: %w", err)
	}

	router.Use(
		middleware.RequestID(opts.Logger),
		middleware.Recover(),
		middleware.AccessLog(),
		middleware.CORS(opts.CORSOrigins),
		middleware.BodyLimit(maxBodyBytes, prefixed(opts.LargeBodyRoutes)),
		middleware.NewRateLimiter(opts.RateLimitPerMinute).Middleware(),
	)
	router.NoRoute(func(c *gin.Context) { apierror.Respond(c, apierror.ErrNotFound) })
	router.NoMethod(func(c *gin.Context) { apierror.Respond(c, apierror.ErrMethodNotAllowed) })

	return router, router.Group(APIPrefix), nil
}

// Serve atiende en port hasta que ctx se cancela, y entonces cierra en orden:
// deja de aceptar conexiones y espera hasta 10 s a las peticiones en curso.
func Serve(ctx context.Context, handler http.Handler, port string, log *slog.Logger) error {
	server := &http.Server{
		Addr:              ":" + port,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      20 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	failed := make(chan error, 1)
	go func() {
		log.Info("server started", "addr", server.Addr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			failed <- err
		}
		close(failed)
	}()

	select {
	case err := <-failed:
		return fmt.Errorf("server stopped: %w", err)
	case <-ctx.Done():
	}

	log.Info("shutting down")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}

func prefixed(routes map[string]int64) map[string]int64 {
	out := make(map[string]int64, len(routes))
	for route, limit := range routes {
		out[APIPrefix+route] = limit
	}
	return out
}
