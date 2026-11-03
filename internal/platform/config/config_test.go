package config

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func validEnv() map[string]string {
	return map[string]string{
		"APP_ENV":                "development",
		"PORT":                   "3001",
		"DATABASE_URL":           "postgresql://orbita_app:secret@localhost:5432/orbita?search_path=orbita",
		"AUTH_PUBLIC_KEY_BASE64": "cHVibGljLWtleQ==",
		"AUTH_ISSUER":            "atm-auth-api",
		"RATE_LIMIT_PER_MINUTE":  "300",
	}
}

func load(env map[string]string) (Config, error) {
	return LoadFrom(func(name string) string { return env[name] })
}

func TestAValidEnvironmentLoads(t *testing.T) {
	cfg, err := load(validEnv())
	require.NoError(t, err)
	assert.Equal(t, Development, cfg.Env)
	assert.Equal(t, "atm-auth-api", cfg.Auth.Issuer)
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

func TestTheAuthKeyAndIssuerAreRequired(t *testing.T) {
	env := validEnv()
	delete(env, "AUTH_PUBLIC_KEY_BASE64")
	delete(env, "AUTH_ISSUER")
	_, err := load(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "AUTH_PUBLIC_KEY_BASE64 is required")
	assert.Contains(t, err.Error(), "AUTH_ISSUER is required")
}

func TestTheDatabasePasswordIsNeverPrinted(t *testing.T) {
	env := validEnv()
	env["DATABASE_URL"] = "mysql://user:topsecret@host/db"
	_, err := load(env)
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "topsecret")
}

func TestProductionNeedsCors(t *testing.T) {
	env := validEnv()
	env["APP_ENV"] = "production"
	_, err := load(env)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "CORS_ORIGIN is required in production")

	env["CORS_ORIGIN"] = "https://orbita.atmosferast.com, https://admin.example.com"
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
