package httpserver

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/health"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/middleware"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/testsupport"
)

type fakeDB struct{ err error }

func (f fakeDB) Ping(context.Context) error { return f.err }

func newTestRouter(t *testing.T, db health.Pinger, perMinute int) *gin.Engine {
	t.Helper()
	gin.SetMode(gin.TestMode)
	router, api, err := NewRouter(Options{
		Logger:             slog.New(slog.NewTextHandler(io.Discard, nil)),
		CORSOrigins:        []string{"https://orbita.atmosferast.com"},
		RateLimitPerMinute: perMinute,
	})
	require.NoError(t, err)
	health.Register(api, db)
	api.GET("/boom", func(*gin.Context) { panic("broken handler") })
	api.POST("/echo", func(c *gin.Context) {
		var payload map[string]any
		if err := c.ShouldBindJSON(&payload); err != nil {
			apierror.Respond(c, err)
			return
		}
		apierror.OK(c, payload)
	})
	return router
}

func do(router http.Handler, req *http.Request) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	return rec
}

type errorBody struct {
	Error struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}

func decodeError(t *testing.T, rec *httptest.ResponseRecorder) errorBody {
	t.Helper()
	var body errorBody
	require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
	return body
}

func TestHealthAnswersWithTheDataEnvelope(t *testing.T) {
	rec := do(newTestRouter(t, fakeDB{}, 300), httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":{"status":"ok"}}`, rec.Body.String())
}

func TestReadyIs503WhenTheDatabaseIsDown(t *testing.T) {
	router := newTestRouter(t, fakeDB{err: errors.New("connection refused")}, 300)

	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))
	assert.Equal(t, http.StatusServiceUnavailable, rec.Code)
	assert.Equal(t, apierror.CodeDatabaseUnavailable, decodeError(t, rec).Error.Code)

	// La liveness sigue en 200: Docker no debe reiniciar el contenedor por la base.
	rec = do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestReadyWithTheRealDatabase(t *testing.T) {
	router := newTestRouter(t, testsupport.Pool(t), 300)
	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health/ready", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
	assert.JSONEq(t, `{"data":{"status":"ok","database":"ok"}}`, rec.Body.String())
}

func TestAPanicIsA500AndTheServerKeepsGoing(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 300)
	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/boom", nil))
	assert.Equal(t, http.StatusInternalServerError, rec.Code)
	body := decodeError(t, rec)
	assert.Equal(t, apierror.CodeInternal, body.Error.Code)
	assert.NotContains(t, rec.Body.String(), "broken handler")

	rec = do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.Equal(t, http.StatusOK, rec.Code)
}

func TestAnUnknownRouteIsNotFoundWithTheErrorEnvelope(t *testing.T) {
	rec := do(newTestRouter(t, fakeDB{}, 300), httptest.NewRequest(http.MethodGet, "/api/v1/nothing", nil))
	assert.Equal(t, http.StatusNotFound, rec.Code)
	body := decodeError(t, rec)
	assert.Equal(t, apierror.CodeNotFound, body.Error.Code)
	assert.NotEmpty(t, body.Error.Message)
}

func TestTheRequestIDOfTheClientComesBack(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 300)

	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(middleware.RequestIDHeader, "android-1234")
	assert.Equal(t, "android-1234", do(router, req).Header().Get(middleware.RequestIDHeader))

	// Uno que podría ensuciar los logs se reemplaza.
	req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set(middleware.RequestIDHeader, "bad id\nwith newline")
	got := do(router, req).Header().Get(middleware.RequestIDHeader)
	assert.NotEmpty(t, got)
	assert.NotContains(t, got, " ")
}

func TestABodyOverTheLimitIs413(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 300)
	big := `{"text":"` + strings.Repeat("a", maxBodyBytes) + `"}`
	req := httptest.NewRequest(http.MethodPost, "/api/v1/echo", strings.NewReader(big))
	req.Header.Set("Content-Type", "application/json")
	rec := do(router, req)
	assert.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
	assert.Equal(t, apierror.CodePayloadTooLarge, decodeError(t, rec).Error.Code)
}

func TestTooManyRequestsFromOneIP(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 3)
	for range 3 {
		rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
		require.Equal(t, http.StatusOK, rec.Code)
	}
	rec := do(router, httptest.NewRequest(http.MethodGet, "/api/v1/health", nil))
	assert.Equal(t, http.StatusTooManyRequests, rec.Code)
	assert.NotEmpty(t, rec.Header().Get("Retry-After"))
	assert.Equal(t, apierror.CodeTooManyRequests, decodeError(t, rec).Error.Code)

	// Otra IP tiene su propio cupo.
	req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.RemoteAddr = "203.0.113.9:5555"
	assert.Equal(t, http.StatusOK, do(router, req).Code)
}

func TestForwardedForIsOnlyTrustedFromTheProxy(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 1)

	// Desde internet, X-Forwarded-For no cambia la IP: no sirve para saltarse el límite.
	for i, forwarded := range []string{"198.51.100.1", "198.51.100.2"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.RemoteAddr = "203.0.113.50:1234"
		req.Header.Set("X-Forwarded-For", forwarded)
		want := http.StatusOK
		if i == 1 {
			want = http.StatusTooManyRequests
		}
		assert.Equal(t, want, do(router, req).Code, forwarded)
	}

	// Desde nginx (loopback), cada cliente real cuenta aparte.
	for _, forwarded := range []string{"198.51.100.3", "198.51.100.4"} {
		req := httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
		req.RemoteAddr = "127.0.0.1:1234"
		req.Header.Set("X-Forwarded-For", forwarded)
		assert.Equal(t, http.StatusOK, do(router, req).Code, forwarded)
	}
}

func TestCORSOnlyForTheConfiguredOrigins(t *testing.T) {
	router := newTestRouter(t, fakeDB{}, 300)

	req := httptest.NewRequest(http.MethodOptions, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://orbita.atmosferast.com")
	rec := do(router, req)
	assert.Equal(t, http.StatusNoContent, rec.Code)
	assert.Equal(t, "https://orbita.atmosferast.com", rec.Header().Get("Access-Control-Allow-Origin"))

	req = httptest.NewRequest(http.MethodGet, "/api/v1/health", nil)
	req.Header.Set("Origin", "https://evil.example.com")
	rec = do(router, req)
	assert.Empty(t, rec.Header().Get("Access-Control-Allow-Origin"))
}
