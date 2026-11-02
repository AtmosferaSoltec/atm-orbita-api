package centralauth

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/apierror"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

// Setup son los ganchos que crean los datos iniciales (perfil, categorías, "Efectivo").
type Setup func(ctx context.Context, userID uuid.UUID, mainCurrency, timezone string) error

// DeletionHook borra lo que la cascada no alcanza (archivos de R2).
type DeletionHook func(ctx context.Context, userID uuid.UUID) error

// Users guarda la fila local de cada persona. Es una sombra de la cuenta de
// atm-auth-api con el mismo id: mientras las tablas apunten a users(id), la
// cascada sigue borrando los datos de quien elimina su cuenta.
type Users struct {
	Tx           tx.Runner
	Setup        Setup
	OnDelete     []DeletionHook
	MainCurrency string
	Timezone     string
}

// Provision crea la fila y los datos iniciales, una sola vez por persona.
func (u Users) Provision(ctx context.Context, userID uuid.UUID, email, name string) error {
	return u.Tx.InTx(ctx, func(ctx context.Context) error {
		if email == "" {
			return fmt.Errorf("token without email")
		}
		tag, err := u.Tx.DB(ctx).Exec(ctx,
			`INSERT INTO users (id, email, name) VALUES ($1, $2, NULLIF($3, '')) ON CONFLICT (id) DO NOTHING`,
			userID, email, name)
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			// Esa persona ya tiene una cuenta propia de Orbita con otro id: hasta migrarla
			// no se pueden juntar, y mezclar datos a ciegas sería peor que negar el acceso.
			return apierror.New(http.StatusConflict, "ACCOUNT_NOT_MIGRATED", "Esta cuenta aún no se migra. Inicia sesión con tu cuenta anterior.")
		}
		if err != nil {
			return fmt.Errorf("insert user: %w", err)
		}
		if tag.RowsAffected() == 0 {
			return nil // ya existe: cuenta migrada o ya vista
		}
		return u.Setup(ctx, userID, u.MainCurrency, u.Timezone)
	})
}

// Delete borra a la persona y sus datos. Es idempotente: si no existe, no hace nada.
func (u Users) Delete(ctx context.Context, userID uuid.UUID) error {
	return u.Tx.InTx(ctx, func(ctx context.Context) error {
		for _, hook := range u.OnDelete {
			if err := hook(ctx, userID); err != nil {
				return fmt.Errorf("deletion hook: %w", err)
			}
		}
		if _, err := u.Tx.DB(ctx).Exec(ctx, `DELETE FROM users WHERE id = $1`, userID); err != nil {
			return fmt.Errorf("delete user: %w", err)
		}
		return nil
	})
}
