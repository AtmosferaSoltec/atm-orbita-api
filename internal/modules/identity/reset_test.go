package identity_test

import (
	"context"
	"net/http"
	"regexp"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

// Integración del plan 11: recuperar la contraseña con un código de 6 dígitos.

var sixDigits = regexp.MustCompile(`\b[0-9]{6}\b`)

// requestCode pide un código y lo saca del último correo capturado.
func (h *harness) requestCode(t *testing.T, email string) string {
	t.Helper()
	before := len(h.mailer.all())
	res := h.call(t, http.MethodPost, "/auth/forgot-password", "", map[string]string{"email": email})
	require.Equal(t, http.StatusNoContent, res.status)
	sent := h.mailer.all()
	require.Len(t, sent, before+1, "llega un correo")
	return sixDigits.FindString(sent[len(sent)-1].Text)
}

func (h *harness) reset(t *testing.T, email, code, newPassword string) response {
	t.Helper()
	return h.call(t, http.MethodPost, "/auth/reset-password", "", map[string]string{
		"email": email, "code": code, "new_password": newPassword,
	})
}

func TestRecoveringThePasswordFromStartToEnd(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("reset")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	old := h.register(t, email)

	code := h.requestCode(t, email)
	require.Len(t, code, 6)
	message := h.mailer.all()[len(h.mailer.all())-1]
	assert.Equal(t, email, message.To)
	assert.Contains(t, message.Subject, code)
	assert.Contains(t, message.HTML, code)
	assert.Contains(t, message.Text, "Vence en 15 minutos")

	// La base guarda el hash, nunca el código.
	var stored []byte
	require.NoError(t, h.pool.QueryRow(context.Background(), `
		SELECT r.code_hash FROM password_reset_codes r JOIN users u ON u.id = r.user_id
		WHERE u.email = $1 AND r.used_at IS NULL`, email).Scan(&stored))
	assert.Len(t, stored, 32)
	assert.NotContains(t, string(stored), code)

	// Verificar no lo consume.
	res := h.call(t, http.MethodPost, "/auth/verify-reset-code", "", map[string]string{"email": email, "code": code})
	assert.Equal(t, http.StatusNoContent, res.status)

	// Una contraseña nueva corta no consume el código.
	res = h.reset(t, email, code, "1234567")
	assert.Equal(t, http.StatusBadRequest, res.status)
	assert.Equal(t, []string{"PASSWORD_TOO_SHORT"}, fieldCodes(res))

	res = h.reset(t, email, code, "otraclave123")
	require.Equal(t, http.StatusNoContent, res.status, res.body)
	sent := h.mailer.all()
	assert.Contains(t, sent[len(sent)-1].Subject, "se cambió")

	// La nueva sirve, la vieja no, y las sesiones anteriores se cerraron.
	assert.Equal(t, http.StatusOK, h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": "otraclave123"}).status)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodPost, "/auth/login", "", map[string]string{"email": email, "password": password}).status)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodGet, "/me", old.access, nil).status)
	assert.Equal(t, http.StatusUnauthorized, h.call(t, http.MethodPost, "/auth/refresh", "", map[string]string{"refresh_token": old.refresh}).status)

	// El código ya se usó.
	res = h.reset(t, email, code, "terceraclave1")
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)
	assert.Equal(t, "RESET_CODE_INVALID", res.errorCode())
}

func TestAnUnknownEmailGetsTheSameAnswerAndNoEmail(t *testing.T) {
	h := newHarness(t, 1000)
	before := len(h.mailer.all())
	res := h.call(t, http.MethodPost, "/auth/forgot-password", "", map[string]string{"email": testsupport.UniqueEmail("nadie")})
	assert.Equal(t, http.StatusNoContent, res.status)
	assert.Len(t, h.mailer.all(), before)

	res = h.call(t, http.MethodPost, "/auth/verify-reset-code", "", map[string]string{"email": testsupport.UniqueEmail("nadie"), "code": "123456"})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status)

	res = h.call(t, http.MethodPost, "/auth/forgot-password", "", map[string]string{"email": "no-es-correo"})
	assert.Equal(t, []string{"EMAIL_INVALID"}, fieldCodes(res))
	res = h.call(t, http.MethodPost, "/auth/verify-reset-code", "", map[string]string{"email": "ana@correo.com", "code": "12a456"})
	assert.Equal(t, []string{"CODE_INVALID"}, fieldCodes(res))
}

func TestACodeExpiresAndOnlyTheNewestOneWorks(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("expire")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)

	first := h.requestCode(t, email)
	second := h.requestCode(t, email)
	if first != second {
		res := h.reset(t, email, first, "otraclave123")
		assert.Equal(t, http.StatusUnprocessableEntity, res.status, "pedir otro invalida el anterior")
	}

	h.clock.advance(16 * time.Minute)
	res := h.call(t, http.MethodPost, "/auth/verify-reset-code", "", map[string]string{"email": email, "code": second})
	assert.Equal(t, http.StatusUnprocessableEntity, res.status, "vencido")
}

func TestFiveWrongAttemptsSpendTheCode(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("attempts")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)
	code := h.requestCode(t, email)

	wrong := "000000"
	if code == wrong {
		wrong = "111111"
	}
	for range 5 {
		res := h.call(t, http.MethodPost, "/auth/verify-reset-code", "", map[string]string{"email": email, "code": wrong})
		require.Equal(t, http.StatusUnprocessableEntity, res.status)
	}
	res := h.reset(t, email, code, "otraclave123")
	assert.Equal(t, http.StatusUnprocessableEntity, res.status, "sin intentos, ni el correcto sirve")
}

func TestTwoResetsAtOnceUseTheCodeOnce(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("resetrace")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)
	code := h.requestCode(t, email)

	statuses := make([]int, 2)
	var wg sync.WaitGroup
	for i := range statuses {
		wg.Go(func() { statuses[i] = h.reset(t, email, code, "otraclave123").status })
	}
	wg.Wait()
	assert.ElementsMatch(t, []int{http.StatusNoContent, http.StatusUnprocessableEntity}, statuses)
}

func TestTheFourthRequestInADaySendsNothing(t *testing.T) {
	h := newHarness(t, 1000)
	email := testsupport.UniqueEmail("sends")
	testsupport.DeleteUsersByEmail(t, h.pool, email)
	h.register(t, email)
	for range 3 {
		h.requestCode(t, email)
	}
	before := len(h.mailer.all())
	res := h.call(t, http.MethodPost, "/auth/forgot-password", "", map[string]string{"email": email})
	assert.Equal(t, http.StatusNoContent, res.status)
	assert.Len(t, h.mailer.all(), before)

	// Al día siguiente vuelve a poder.
	h.clock.advance(25 * time.Hour)
	h.requestCode(t, email)
}

func fieldCodes(r response) []string {
	e, _ := r.body["error"].(map[string]any)
	details, _ := e["details"].([]any)
	var codes []string
	for _, d := range details {
		codes = append(codes, d.(map[string]any)["code"].(string))
	}
	return codes
}
