package config

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENV":                     "development",
		"PORT":                        "3001",
		"DATABASE_URL":                "postgresql://orbita_app:secret@localhost:5432/orbita?search_path=orbita",
		"JWT_ACCESS_SECRET":           strings.Repeat("s", 64),
		"ACCESS_TOKEN_TTL_SECONDS":    "900",
		"REFRESH_TOKEN_TTL_DAYS":      "30",
		"RATE_LIMIT_PER_MINUTE":       "300",
		"LOGIN_RATE_LIMIT_PER_MINUTE": "5",
	}
}

func load(env map[string]string) (Config, error) {
	return LoadFrom(func(name string) string { return env[name] })
}

func TestAValidEnvironmentLoads(t *testing.T) {
	cfg, err := load(validEnv())
	require.NoError(t, err)
	assert.Equal(t, Development, cfg.Env)
	assert.Equal(t, 15*time.Minute, cfg.AccessTokenTTL)
	assert.Equal(t, 30*24*time.Hour, cfg.RefreshTokenTTL)
	assert.Empty(t, cfg.CORSOrigins)
}

func TestAMissingVariableIsNamed(t *testing.T) {
	env := validEnv()
	delete(env, "DATABASE_URL")
	_, err := load(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "DATABASE_URL is required")
}

func TestEveryProblemIsReportedAtOnce(t *testing.T) {
	env := validEnv()
	delete(env, "DATABASE_URL")
	delete(env, "PORT")
	env["RATE_LIMIT_PER_MINUTE"] = "many"
	_, err := load(env)
	require.Error(t, err)
	for _, name := range []string{"DATABASE_URL", "PORT", "RATE_LIMIT_PER_MINUTE"} {
		assert.Contains(t, err.Error(), name)
	}
}

func TestAShortSecretIsRejected(t *testing.T) {
	env := validEnv()
	env["JWT_ACCESS_SECRET"] = "short"
	_, err := load(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "JWT_ACCESS_SECRET must have at least 32 characters")
}

func TestTheDatabasePasswordIsNeverPrinted(t *testing.T) {
	env := validEnv()
	env["DATABASE_URL"] = "mysql://user:topsecret@host/db"
	_, err := load(env)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestProductionNeedsMailAndCors(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "production"
	_, err := load(env)
	require.Error(t, err)
	for _, name := range []string{"CORS_ORIGIN", "RESEND_API_KEY", "MAIL_FROM"} {
		assert.Contains(t, err.Error(), name+" is required in production")
	}

	env["CORS_ORIGIN"] = "https://orbita.atmosferast.com, https://admin.example.com"
	env["RESEND_API_KEY"] = "re_123"
	env["MAIL_FROM"] = "Orbita <no-reply@atmosferast.com>"
	cfg, err := load(env)
	require.NoError(t, err)
	assert.True(t, cfg.IsProduction())
	assert.Equal(t, []string{"https://orbita.atmosferast.com", "https://admin.example.com"}, cfg.CORSOrigins)
}

func TestAnUnknownEnvironmentIsRejected(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "staging"
	_, err := load(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "APP_ENV")
}
