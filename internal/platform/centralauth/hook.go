package centralauth

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// UserDeletedRoute monta POST /internal/auth/user-deleted: atm-auth-api lo llama
// antes de borrar la cuenta de una persona (ACCOUNT_DELETION_HOOKS). Solo acepta
// el token de servicio de atm-auth-api; si algo falla responde error y auth no
// borra nada, así que reintentar es seguro (borrar dos veces no hace daño).
func UserDeletedRoute(r gin.IRoutes, verifier *Verifier, remove func(c *gin.Context, userID uuid.UUID) error) {
	r.POST("/internal/auth/user-deleted", func(c *gin.Context) {
		header := c.GetHeader("Authorization")
		scheme, token, found := strings.Cut(header, " ")
		if !found || !strings.EqualFold(scheme, "Bearer") {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		claims, err := verifier.Verify(strings.TrimSpace(token))
		if err != nil || claims.Role != "service" || claims.Subject != "system:auth" {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		var body struct {
			UserID string `json:"user_id"`
		}
		userID, perr := uuid.Parse("")
		if err := c.ShouldBindJSON(&body); err == nil {
			userID, perr = uuid.Parse(body.UserID)
		}
		if perr != nil {
			apierror.Respond(c, apierror.New(http.StatusBadRequest, apierror.CodeValidationFailed, "Falta el user_id."))
			return
		}
		if err := remove(c, userID); err != nil {
			apierror.Respond(c, err)
			return
		}
		c.Status(http.StatusNoContent)
	})
}

// MeRoute monta GET /me: quién es la persona autenticada, según la fila local.
func MeRoute(protected gin.IRoutes, db func(ctx context.Context) tx.DBTX) {
	protected.GET("/me", func(c *gin.Context) {
		principal, _ := auth.FromContext(c.Request.Context())
		var out struct {
			ID        uuid.UUID `json:"id"`
			Email     string    `json:"email"`
			Name      *string   `json:"name"`
			CreatedAt time.Time `json:"created_at"`
		}
		err := db(c.Request.Context()).QueryRow(c.Request.Context(),
			`SELECT id, email, name, created_at FROM users WHERE id = $1`, principal.UserID).
			Scan(&out.ID, &out.Email, &out.Name, &out.CreatedAt)
		if errors.Is(err, pgx.ErrNoRows) {
			apierror.Respond(c, apierror.ErrUnauthorized)
			return
		}
		if err != nil {
			apierror.Respond(c, err)
			return
		}
		out.CreatedAt = out.CreatedAt.UTC()
		apierror.OK(c, out)
	})
}
