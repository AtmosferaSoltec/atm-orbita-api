package centralauth

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/auth"
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
			apierror.Respond(c, auth.ErrUnauthorized)
			return
		}
		claims, err := verifier.Verify(strings.TrimSpace(token))
		if err != nil || claims.Role != "service" || claims.Subject != "system:auth" {
			apierror.Respond(c, auth.ErrUnauthorized)
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
