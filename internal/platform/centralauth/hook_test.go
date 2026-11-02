package centralauth

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
)

func TestUserDeletedRouteOnlyAcceptsAuthsServiceToken(t *testing.T) {
	gin.SetMode(gin.TestMode)
	f := newFixture(t)
	var removed []uuid.UUID
	r := gin.New()
	UserDeletedRoute(r, f.v, func(_ *gin.Context, id uuid.UUID) error { removed = append(removed, id); return nil })

	id := uuid.New()
	call := func(token, body string) int {
		req := httptest.NewRequest(http.MethodPost, "/internal/auth/user-deleted", strings.NewReader(body))
		req.Header.Set("Content-Type", "application/json")
		if token != "" {
			req.Header.Set("Authorization", "Bearer "+token)
		}
		rec := httptest.NewRecorder()
		r.ServeHTTP(rec, req)
		return rec.Code
	}
	body := `{"user_id":"` + id.String() + `"}`
	service := f.token(t, Claims{Role: "service", System: "auth", RegisteredClaims: jwt.RegisteredClaims{Subject: "system:auth"}})
	otherService := f.token(t, Claims{Role: "service", System: "pulso", RegisteredClaims: jwt.RegisteredClaims{Subject: "system:pulso"}})
	user := f.token(t, Claims{Role: "user", System: "orbita", RegisteredClaims: jwt.RegisteredClaims{Subject: id.String()}})

	assert.Equal(t, http.StatusUnauthorized, call("", body))
	assert.Equal(t, http.StatusUnauthorized, call(user, body))
	assert.Equal(t, http.StatusUnauthorized, call(otherService, body))
	assert.Empty(t, removed)
	assert.Equal(t, http.StatusBadRequest, call(service, `{"user_id":"x"}`))
	assert.Equal(t, http.StatusNoContent, call(service, body))
	assert.Equal(t, []uuid.UUID{id}, removed)
}
