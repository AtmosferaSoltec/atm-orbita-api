// Punto de entrada único del API: carga la configuración, arma los módulos y
// arranca Gin. El mismo binario tiene los subcomandos que usan Docker y el
// compose de producción:
//
//	api                     arranca el servidor
//	api migrate [up|down|status]
//	api healthcheck         lo usa el HEALTHCHECK de la imagen (no trae curl)
package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	// La imagen trae tzdata, pero así "hoy" por zona horaria no depende del sistema.
	_ "time/tzdata"

	"github.com/AtmosferaSoltec/atm-orbita-api/db"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/config"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "healthcheck":
			os.Exit(healthcheck())
		case "migrate":
			os.Exit(migrate(os.Args[2:]))
		default:
			fmt.Fprintf(os.Stderr, "unknown command %q\n", os.Args[1])
			os.Exit(2)
		}
	}
	os.Exit(run())
}

func run() int {
	cfg, err := config.Load()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	log := logger.New(os.Stdout, cfg.IsProduction())
	slog.SetDefault(log)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := database.Open(ctx, cfg.DatabaseURL)
	if err != nil {
		log.Error("database", "error", err)
		return 1
	}
	defer pool.Close()
	// No se exige la base para arrancar: /health/ready dice si responde, y el
	// pool se reconecta solo cuando vuelve.
	if err := database.Ping(ctx, pool, 5*time.Second); err != nil {
		log.Warn("database not reachable at startup", "error", err)
	}

	router, api, err := httpserver.NewRouter(httpserver.Options{
		Production:         cfg.IsProduction(),
		Logger:             log,
		CORSOrigins:        cfg.CORSOrigins,
		RateLimitPerMinute: cfg.RateLimitPerMinute,
	})
	if err != nil {
		log.Error("router", "error", err)
		return 1
	}
	health.Register(api, pool)

	runner := tx.NewRunner(pool)
	identityModule, err := identity.New(identity.Deps{
		Tx:              runner,
		Clock:           clock.System{},
		JWTSecret:       cfg.JWTAccessSecret,
		AccessTokenTTL:  cfg.AccessTokenTTL,
		RefreshTokenTTL: cfg.RefreshTokenTTL,
	})
	if err != nil {
		log.Error("modules", "error", err)
		return 1
	}

	profileModule := profile.New(runner, clock.System{})
	ledgerModule := ledger.New(runner, clock.System{}, profileModule.Reader())
	reportsModule := reports.New(ledgerModule.ReportQuery(), profileModule.Reader())

	// El alta corre en una sola transacción: cuenta, sesión y, por cada módulo, lo
	// que el usuario necesita desde el primer día.
	identityModule.OnUserRegistered(func(ctx context.Context, user identity.NewUser) error {
		if err := profileModule.OnUserRegistered(ctx, user.ID, user.MainCurrency, user.Timezone); err != nil {
			return err
		}
		return ledgerModule.OnUserRegistered(ctx, user.ID, user.MainCurrency)
	})

	// Registro, inicio de sesión y renovación tienen un límite por IP más estrecho.
	limited := api.Group("", middleware.NewRateLimiter(cfg.LoginRateLimitPerMinute).Middleware())
	protected := api.Group("", auth.Require(identityModule.Authenticator()))
	identityModule.Routes(api, limited, protected)
	profileModule.Routes(api, protected)
	ledgerModule.Routes(protected)
	reportsModule.Routes(protected)

	if err := httpserver.Serve(ctx, router, cfg.Port, log); err != nil {
		log.Error("server", "error", err)
		return 1
	}
	return 0
}

// migrate lo invoca el servicio migrate del compose antes de arrancar la app.
// Solo necesita DATABASE_URL.
func migrate(args []string) int {
	log := logger.New(os.Stdout, os.Getenv("APP_ENV") == string(config.Production))
	command := database.MigrateUp
	if len(args) > 0 {
		command = args[0]
	}
	databaseURL, err := config.DatabaseURL()
	if err != nil {
		log.Error("migrate", "error", err)
		return 1
	}
	if err := database.Migrate(context.Background(), databaseURL, db.Migrations, command, log); err != nil {
		log.Error("migrate", "error", err)
		return 1
	}
	return 0
}

func healthcheck() int {
	client := http.Client{Timeout: 3 * time.Second}
	res, err := client.Get("http://127.0.0.1:" + port() + httpserver.APIPrefix + "/health")
	if err != nil {
		return 1
	}
	defer func() { _ = res.Body.Close() }()
	if res.StatusCode != http.StatusOK {
		return 1
	}
	return 0
}

func port() string {
	if p := os.Getenv("PORT"); p != "" {
		return p
	}
	return "3001"
}
