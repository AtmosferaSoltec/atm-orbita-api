package identity_test

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

// Integración del plan 12: eliminar la cuenta. Que no quede ninguna fila del
// usuario en ninguna tabla lo prueba ledger (account_deletion_test.go), donde
// están cableados todos los módulos.

func (h *harness) deleteAccount(t *testing.T, access, pass string) response {
	t.Helper()
	return h.call(t, http.MethodDelete, "/me", access, map[string]string{"password": pass})
}

func (h *harness) userExists(t *testing.T, email string) bool {
	t.Helper()
	var exists bool
	require.NoError(t, h.pool.QueryRow(context.Background(),
		`SELECT EXISTS (SELECT 1 FROM users WHERE email = $1)`, email).Scan(&exists))
	return exists
}

func TestDeletingTheAccountEndsEverythingAndFreesTheEmail(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("delete")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	first := h.register(t, email)
	other := h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password})
	require.Equal(t, http.StatusOK, other.status)
	second := tokensOf(t, other.data(t))

	res := h.deleteAccount(t, first.access, password)
	require.Equal(t, http.StatusNoContent, res.status, res.body)
	assert.False(t, h.userExists(t, email))

	// Ninguna sesión sobrevive, tampoco la de otro dispositivo.
	for _, s := range []tokens{first, second} {
		assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", s.access, nil).status)
		assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": s.refresh}).status)
	}
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password}).status)

	// El correo queda libre: registrarse de nuevo empieza de cero.
	h.register(t, email)
}

func TestDeletingTheAccountAsksForTheRightPassword(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("delpass")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	session := h.register(t, email)

	res := h.deleteAccount(t, session.access, "")
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"PASSWORD_REQUIRED"}, fieldCodes(res))

	res = h.deleteAccount(t, session.access, "otraclave123")
	assert.Equal(t, http.StatusUnauthorized, res.status)
	assert.Equal(t, "INVALID_CREDENTIALS", res.errorCode())

	res = h.call(t, http.MethodDelete, "/me", "", map[string]string{"password": password})
	assert.Equal(t, http.StatusUnauthorized, res.status, "sin token")

	assert.True(t, h.userExists(t, email))
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", session.access, nil).status, "la sesión sigue")
}

func TestTooManyWrongPasswordsWhenDeletingWait(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("delwait")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	session := h.register(t, email)

	for range 5 {
		require.Equal(t, http.StatusUnauthorized, h.deleteAccount(t, session.access, "otraclave123").status)
	}
	res := h.deleteAccount(t, session.access, password)
	assert.Equal(t, http.StatusTooManyRequests, res.status, "ni la correcta mientras espera")
	assert.True(t, h.userExists(t, email))
}

func TestAFailureHalfwayDeletesNothing(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("delfail")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	session := h.register(t, email)
	h.module.OnUserDeleted(func(context.Context, uuid.UUID) error { return errors.New("storage down") })

	res := h.deleteAccount(t, session.access, password)
	assert.Equal(t, http.StatusInternalServerError, res.status)
	assert.True(t, h.userExists(t, email))
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodGet, "/me", session.access, nil).status)
}
