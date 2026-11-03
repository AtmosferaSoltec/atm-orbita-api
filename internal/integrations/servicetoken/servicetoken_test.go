package servicetoken_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/integrations/servicetoken"
)

type fakeAuth struct {
	*httptest.Server
	calls   atomic.Int32
	status  int
	expires int
}

func newFakeAuth(t *testing.T) *fakeAuth {
	t.Helper()
	f := &fakeAuth{status: http.StatusOK, expires: 900}
	f.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := f.calls.Add(1)
		var body map[string]string
		_ = json.NewDecoder(r.Body).Decode(&body)
		if r.URL.Path != "/v1/token" || r.Method != http.MethodPost || body["client_id"] != "orbita" || body["client_secret"] != "s3cret" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if f.status != http.StatusOK {
			w.WriteHeader(f.status)
			_, _ = w.Write([]byte(`{"error":"nope"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"access_token": "token-" + string(rune('0'+n)), "expires_in": f.expires})
	}))
	t.Cleanup(f.Close)
	return f
}

type clock struct {
	mu  sync.Mutex
	now time.Time
}

func (c *clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.now }
func (c *clock) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.now = c.now.Add(d)
}

func source(f *fakeAuth, c *clock) *servicetoken.Source {
	return servicetoken.New(servicetoken.Config{AuthURL: f.URL + "/", ClientID: "orbita", ClientSecret: "s3cret", Now: c.Now})
}

func TestTokenIsReusedUntilItIsAboutToExpire(t *testing.T) {
	f := newFakeAuth(t)
	c := &clock{now: time.Now()}
	s := source(f, c)

	first, err := s.Token(context.Background())
	require.NoError(t, err)
	again, err := s.Token(context.Background())
	require.NoError(t, err)
	assert.Equal(t, first, again)
	assert.EqualValues(t, 1, f.calls.Load())

	c.advance(900*time.Second - 61*time.Second)
	_, err = s.Token(context.Background())
	require.NoError(t, err)
	assert.EqualValues(t, 1, f.calls.Load(), "still valid 61 s before expiry")

	c.advance(2 * time.Second)
	renewed, err := s.Token(context.Background())
	require.NoError(t, err)
	assert.NotEqual(t, first, renewed)
	assert.EqualValues(t, 2, f.calls.Load(), "refreshed inside the last 60 s")
}

func TestConcurrentCallsShareOneRequest(t *testing.T) {
	f := newFakeAuth(t)
	s := source(f, &clock{now: time.Now()})
	var wg sync.WaitGroup
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := s.Token(context.Background())
			assert.NoError(t, err)
		}()
	}
	wg.Wait()
	assert.EqualValues(t, 1, f.calls.Load())
}

func TestAuthFailureIsAnErrorAndNotCached(t *testing.T) {
	f := newFakeAuth(t)
	f.status = http.StatusUnauthorized
	s := source(f, &clock{now: time.Now()})
	_, err := s.Token(context.Background())
	require.Error(t, err)
	assert.NotContains(t, err.Error(), "s3cret")

	f.status = http.StatusOK
	token, err := s.Token(context.Background())
	require.NoError(t, err)
	assert.NotEmpty(t, token)
}

func TestWithoutSecretItIsNotConfigured(t *testing.T) {
	s := servicetoken.New(servicetoken.Config{AuthURL: "http://x", ClientID: "orbita"})
	assert.False(t, s.Configured())
	_, err := s.Token(context.Background())
	assert.ErrorIs(t, err, servicetoken.ErrNotConfigured)
}

func TestDoRenewsTheTokenOnceAfterA401(t *testing.T) {
	f := newFakeAuth(t)
	s := source(f, &clock{now: time.Now()})
	var seen []string
	downstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seen = append(seen, r.Header.Get("Authorization"))
		if len(seen) == 1 {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer downstream.Close()

	res, err := s.Do(context.Background(), http.DefaultClient, func() (*http.Request, error) {
		return http.NewRequest(http.MethodGet, downstream.URL, nil)
	})
	require.NoError(t, err)
	_ = res.Body.Close()
	assert.Equal(t, http.StatusNoContent, res.StatusCode)
	assert.Equal(t, []string{"Bearer token-1", "Bearer token-2"}, seen)
}
