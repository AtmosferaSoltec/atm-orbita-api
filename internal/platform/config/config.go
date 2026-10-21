// Package config lee y valida las variables de entorno al arrancar. Si falta
// una obligatoria o tiene mal formato, la app no arranca: un .env incompleto se
// nota en el despliegue, no atendiendo a una persona.
package config

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"
)

// Environment es el valor de APP_ENV.
type Environment string

// Entornos admitidos.
const (
	Development Environment = "development"
	Production  Environment = "production"
	Test        Environment = "test"
)

// minSecretLength: con openssl rand -base64 48 salen 64 caracteres.
const minSecretLength = 32

// Config es el entorno ya validado.
type Config struct {
	Env                     Environment
	Port                    string
	DatabaseURL             string
	JWTAccessSecret         string
	AccessTokenTTL          time.Duration
	RefreshTokenTTL         time.Duration
	CORSOrigins             []string
	RateLimitPerMinute      int
	LoginRateLimitPerMinute int
	Mail                    MailConfig
}

// MailConfig son los datos de Resend. Vacíos en desarrollo: el correo va al log.
type MailConfig struct {
	ResendAPIKey string
	From         string
}

// IsProduction dice si corre en el servidor.
func (c Config) IsProduction() bool { return c.Env == Production }

// Load lee el entorno del proceso.
func Load() (Config, error) {
	return LoadFrom(os.Getenv)
}

// LoadFrom lee las variables con lookup, para poder probarlo sin tocar el entorno.
func LoadFrom(lookup func(string) string) (Config, error) {
	r := reader{lookup: lookup}

	cfg := Config{
		Env:                     r.environment("APP_ENV"),
		Port:                    r.port("PORT"),
		DatabaseURL:             r.databaseURL("DATABASE_URL"),
		JWTAccessSecret:         r.secret("JWT_ACCESS_SECRET"),
		AccessTokenTTL:          time.Duration(r.positiveInt("ACCESS_TOKEN_TTL_SECONDS")) * time.Second,
		RefreshTokenTTL:         time.Duration(r.positiveInt("REFRESH_TOKEN_TTL_DAYS")) * 24 * time.Hour,
		CORSOrigins:             r.list("CORS_ORIGIN"),
		RateLimitPerMinute:      r.positiveInt("RATE_LIMIT_PER_MINUTE"),
		LoginRateLimitPerMinute: r.positiveInt("LOGIN_RATE_LIMIT_PER_MINUTE"),
		Mail: MailConfig{
			ResendAPIKey: r.optional("RESEND_API_KEY"),
			From:         r.optional("MAIL_FROM"),
		},
	}

	if cfg.IsProduction() {
		r.requireInProduction("CORS_ORIGIN", len(cfg.CORSOrigins) > 0)
		r.requireInProduction("RESEND_API_KEY", cfg.Mail.ResendAPIKey != "")
		r.requireInProduction("MAIL_FROM", cfg.Mail.From != "")
	}

	if len(r.problems) > 0 {
		return Config{}, fmt.Errorf("invalid environment:\n  - %s", strings.Join(r.problems, "\n  - "))
	}
	return cfg, nil
}

// DatabaseURL es lo único que necesita el subcomando migrate.
func DatabaseURL() (string, error) {
	r := reader{lookup: os.Getenv}
	value := r.databaseURL("DATABASE_URL")
	if len(r.problems) > 0 {
		return "", errors.New(r.problems[0])
	}
	return value, nil
}

type reader struct {
	lookup   func(string) string
	problems []string
}

func (r *reader) fail(format string, args ...any) {
	r.problems = append(r.problems, fmt.Sprintf(format, args...))
}

func (r *reader) required(name string) string {
	value := strings.TrimSpace(r.lookup(name))
	if value == "" {
		r.fail("%s is required", name)
	}
	return value
}

func (r *reader) optional(name string) string {
	return strings.TrimSpace(r.lookup(name))
}

func (r *reader) environment(name string) Environment {
	value := Environment(r.required(name))
	switch value {
	case Development, Production, Test, "":
	default:
		r.fail("%s must be development, production or test (got %q)", name, value)
	}
	return value
}

func (r *reader) port(name string) string {
	value := r.required(name)
	if value == "" {
		return value
	}
	if n, err := strconv.Atoi(value); err != nil || n < 1 || n > 65535 {
		r.fail("%s must be a port number (got %q)", name, value)
	}
	return value
}

func (r *reader) databaseURL(name string) string {
	value := r.required(name)
	if value == "" {
		return value
	}
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "postgres" && parsed.Scheme != "postgresql") || parsed.Host == "" {
		// Nunca se imprime el valor: lleva la contraseña.
		r.fail("%s must be a postgresql:// URL", name)
	}
	return value
}

func (r *reader) secret(name string) string {
	value := r.required(name)
	if value != "" && len(value) < minSecretLength {
		r.fail("%s must have at least %d characters (generate it with: openssl rand -base64 48)", name, minSecretLength)
	}
	return value
}

func (r *reader) positiveInt(name string) int {
	value := r.required(name)
	if value == "" {
		return 0
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		r.fail("%s must be a positive whole number (got %q)", name, value)
	}
	return n
}

func (r *reader) list(name string) []string {
	var items []string
	for _, item := range strings.Split(r.optional(name), ",") {
		if item = strings.TrimSpace(item); item != "" {
			items = append(items, item)
		}
	}
	return items
}

func (r *reader) requireInProduction(name string, present bool) {
	if !present {
		r.fail("%s is required in production", name)
	}
}
