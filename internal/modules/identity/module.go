// Package identity son las cuentas de usuario y sus sesiones: registro, inicio
// y cierre de sesión, renovación y la autenticación de cada petición.
//
// Es la API pública del módulo: los demás módulos y main solo usan lo de aquí.
package identity

import (
	"context"
	"fmt"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/mail"
	identityhttp "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/adapters/http"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/adapters/postgres"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/adapters/security"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/clock"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// NewUser es lo que reciben los ganchos de alta.
type NewUser = app.NewUser

// RegistrationHook corre dentro de la transacción del registro.
type RegistrationHook = app.RegistrationHook

// PasswordParams son los parámetros de argon2id.
type PasswordParams = security.Argon2Params

// FastPasswordParams bajan el costo de argon2id: solo para pruebas, que miden la
// lógica y no la resistencia del hash.
var FastPasswordParams = PasswordParams{MemoryKiB: 1024, Iterations: 1, Parallelism: 1, SaltLength: 16, KeyLength: 32}

// Deps son las dependencias del módulo.
type Deps struct {
	Tx              tx.Runner
	Clock           clock.Clock
	JWTSecret       string
	AccessTokenTTL  time.Duration
	RefreshTokenTTL time.Duration
	// PasswordParams permite bajar el costo de argon2 en pruebas; vacío usa el de producción.
	PasswordParams *PasswordParams
	// Mailer envía los correos de recuperar la contraseña (en segundo plano).
	Mailer mail.Mailer
	// Background corre trabajo fuera de la petición; vacío usa una goroutine. Las
	// pruebas lo hacen síncrono.
	Background func(func())
}

// Module arma el módulo con sus adaptadores.
type Module struct {
	service *app.Service
	handler *identityhttp.Handler
}

// New crea el módulo.
func New(deps Deps) (*Module, error) {
	params := security.DefaultArgon2Params
	if deps.PasswordParams != nil {
		params = *deps.PasswordParams
	}
	service, err := app.NewService(app.Deps{
		Users:       postgres.NewUsers(deps.Tx),
		Sessions:    postgres.NewSessions(deps.Tx),
		ResetCodes:  postgres.NewResetCodes(deps.Tx),
		Hasher:      security.Argon2Hasher{Params: params},
		Tokens:      security.JWTTokens{Secret: []byte(deps.JWTSecret), TTL: deps.AccessTokenTTL},
		Mailer:      resetMailer{deps.Mailer},
		Tx:          deps.Tx,
		Clock:       deps.Clock,
		Config:      app.Config{AccessTokenTTL: deps.AccessTokenTTL, RefreshTokenTTL: deps.RefreshTokenTTL},
		ResetSecret: []byte(deps.JWTSecret),
		Background:  deps.Background,
	})
	if err != nil {
		return nil, fmt.Errorf("identity: %w", err)
	}
	return &Module{service: service, handler: identityhttp.NewHandler(service)}, nil
}

// Routes monta las rutas del módulo.
func (m *Module) Routes(public, limited, protected gin.IRoutes) {
	m.handler.Routes(public, limited, protected)
}

// Authenticator valida los access tokens para el middleware auth.Require.
func (m *Module) Authenticator() auth.Authenticator { return m.service }

// OnUserRegistered agrega un gancho al alta (perfil, categorías, cuenta "Efectivo").
func (m *Module) OnUserRegistered(hook RegistrationHook) { m.service.OnUserRegistered(hook) }

// resetMailer arma los correos de la recuperación y los deja en el Mailer.
type resetMailer struct{ mailer mail.Mailer }

func (m resetMailer) SendResetCode(ctx context.Context, to, code string, validFor time.Duration) error {
	message, err := mail.ResetCodeMessage(to, code, validFor)
	if err != nil {
		return err
	}
	return m.mailer.Send(ctx, message)
}

func (m resetMailer) SendPasswordChanged(ctx context.Context, to string) error {
	message, err := mail.PasswordChangedMessage(to)
	if err != nil {
		return err
	}
	return m.mailer.Send(ctx, message)
}
