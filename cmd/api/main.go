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

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/db"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/documents"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/servicetoken"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/storage"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/credit"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/home"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/ledger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/profile"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/receipts"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/services"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/centralauth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/config"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/database"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/httpserver"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// reportPDFsPerHour es el límite de PDF del reporte por persona (plan 15).
const reportPDFsPerHour = 10

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
		LargeBodyRoutes:    map[string]int64{receipts.UploadPath: receipts.MaxUploadBytes},
	})
	if err != nil {
		log.Error("router", "error", err)
		return 1
	}
	health.Register(api, pool)

	runner := tx.NewRunner(pool)
	profileModule := profile.New(runner, clock.System{})
	ledgerModule := ledger.New(runner, clock.System{}, profileModule.Reader())
	// Token de servicio del sistema orbita: lo comparten storage-api (archivos de los
	// recibos) y documents-api (PDF del reporte). Sin AUTH_CLIENT_SECRET ambos quedan
	// apagados (503) y el resto del API funciona.
	serviceTokens := servicetoken.New(servicetoken.Config{
		AuthURL: cfg.Auth.URL, ClientID: cfg.Auth.ClientID, ClientSecret: cfg.Auth.ClientSecret,
	})
	documentsClient := documents.NewClient(cfg.Services.DocumentsURL, serviceTokens)
	if !documentsClient.Configured() {
		log.Warn("service token not configured: report PDF is disabled")
	}
	reportsModule := reports.New(ledgerModule.ReportQuery(), profileModule.Reader(), reports.PDFOptions{
		Documents: documentsClient,
		Quota:     middleware.NewQuotaLimiter(reportPDFsPerHour, time.Hour),
	})
	creditModule := credit.New(runner, clock.System{}, ledgerModule.CategoryReader(), profileModule.Reader(), ledgerModule.PaymentLedger())
	ledgerModule.OnPaymentDeleted(creditModule)
	homeModule := home.New(profileModule.Reader(), ledgerModule.HomeReader(), reportsModule.Reader(), creditModule.CreditSummary())

	// Archivos de los recibos (plan 18): atm-storage-api con el token de servicio.
	var files storage.Storage = storage.Unavailable{}
	if api := storage.NewStorageAPI(cfg.Services.StorageURL, serviceTokens); api.Configured() {
		files = api
	} else {
		log.Warn("service token not configured: receipt files are disabled")
	}
	receiptsModule := receipts.New(runner, clock.System{}, profileModule.Reader(), files)
	loansModule := loans.New(runner, clock.System{}, profileModule.Reader())
	servicesModule := services.New(runner, clock.System{}, profileModule.Reader())

	// Las personas llegan con el token RS256 de atm-auth-api. La primera vez que una
	// persona hace una petición se crea su fila local, su perfil y sus datos iniciales.
	verifier, err := centralauth.NewVerifier(cfg.Auth.PublicKeyBase64, cfg.Auth.Issuer)
	if err != nil {
		log.Error("central auth", "error", err)
		return 1
	}
	people := centralauth.Users{
		Tx: runner,
		Setup: func(ctx context.Context, userID uuid.UUID, mainCurrency, timezone string) error {
			if err := profileModule.OnUserRegistered(ctx, userID, mainCurrency, timezone); err != nil {
				return err
			}
			return ledgerModule.OnUserRegistered(ctx, userID, mainCurrency)
		},
		OnDelete:     []centralauth.DeletionHook{receiptsModule.OnUserDeleted},
		MainCurrency: currency.Default, Timezone: "America/Lima",
	}
	authenticator := &centralauth.Authenticator{Verifier: verifier, Provisioner: people, Fallback: noOwnTokens{}}
	// atm-auth-api llama aquí antes de borrar la cuenta de una persona (ACCOUNT_DELETION_HOOKS).
	centralauth.UserDeletedRoute(api, verifier, func(c *gin.Context, userID uuid.UUID) error {
		return people.Delete(c.Request.Context(), userID)
	})
	protected := api.Group("", auth.Require(authenticator))
	centralauth.MeRoute(protected, runner.DB)
	profileModule.Routes(api, protected)
	ledgerModule.Routes(protected)
	reportsModule.Routes(protected)
	creditModule.Routes(protected)
	homeModule.Routes(protected)
	receiptsModule.Routes(protected)
	servicesModule.Routes(protected)
	loansModule.Routes(api, protected)

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

// noOwnTokens: Orbita ya no emite tokens propios; todo lo que no venga de atm-auth-api se rechaza.
type noOwnTokens struct{}

func (noOwnTokens) Authenticate(context.Context, string) (auth.Principal, error) {
	return auth.Principal{}, auth.ErrUnauthorized
}
