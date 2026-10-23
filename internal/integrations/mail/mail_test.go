package mail

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestTheResetCodeEmailHasTheCodeInBothVersions(t *testing.T) {
	message, err := ResetCodeMessage("ana@correo.com", "042317", 15*time.Minute)
	require.NoError(t, err)
	assert.Equal(t, "ana@correo.com", message.To)
	assert.Equal(t, "Tu código de Orbita: 042317", message.Subject)
	assert.Contains(t, message.HTML, "042317")
	assert.Contains(t, message.HTML, "Vence en 15 minutos")
	assert.Contains(t, message.Text, "042317")
	assert.NotContains(t, message.HTML, "{{")

	changed, err := PasswordChangedMessage("ana@correo.com")
	require.NoError(t, err)
	assert.Contains(t, changed.HTML, "Tu contraseña se cambió")
	assert.Contains(t, changed.Text, "Si no fuiste tú")
}

func TestResendSendsTheMessageWithTheKey(t *testing.T) {
	var got map[string]any
	var auth string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		body, _ := io.ReadAll(r.Body)
		_ = json.Unmarshal(body, &got)
		_, _ = w.Write([]byte(`{"id":"1"}`))
	}))
	defer server.Close()

	mailer := Resend{APIKey: "re_test", From: "Orbita <no-reply@atmosferast.com>", Endpoint: server.URL}
	require.NoError(t, mailer.Send(context.Background(), Message{To: "ana@correo.com", Subject: "Hola", HTML: "<p>Hola</p>", Text: "Hola"}))
	assert.Equal(t, "Bearer re_test", auth)
	assert.Equal(t, "Orbita <no-reply@atmosferast.com>", got["from"])
	assert.Equal(t, []any{"ana@correo.com"}, got["to"])

	failing := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnprocessableEntity)
	}))
	defer failing.Close()
	mailer.Endpoint = failing.URL
	assert.Error(t, mailer.Send(context.Background(), Message{To: "ana@correo.com"}))
}

type flakyMailer struct {
	mu       sync.Mutex
	failures int
	calls    int
	sent     []Message
}

func (f *flakyMailer) Send(_ context.Context, m Message) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	if f.calls <= f.failures {
		return errors.New("provider down")
	}
	f.sent = append(f.sent, m)
	return nil
}

func TestTheQueueRetriesAndDrainsOnClose(t *testing.T) {
	flaky := &flakyMailer{failures: 2}
	queue := NewQueue(flaky, slog.New(slog.NewTextHandler(io.Discard, nil)), 4, time.Millisecond)
	require.NoError(t, queue.Send(context.Background(), Message{To: "ana@correo.com", Subject: strings.Repeat("a", 3)}))
	queue.Close(context.Background())

	assert.Equal(t, 3, flaky.calls, "dos fallos y el tercero sale")
	assert.Len(t, flaky.sent, 1)

	gaveUp := &flakyMailer{failures: 10}
	queue = NewQueue(gaveUp, slog.New(slog.NewTextHandler(io.Discard, nil)), 3, time.Millisecond)
	require.NoError(t, queue.Send(context.Background(), Message{To: "ana@correo.com"}))
	queue.Close(context.Background())
	assert.Equal(t, 3, gaveUp.calls, "se rinde tras 3 intentos")
	assert.Empty(t, gaveUp.sent)
}
