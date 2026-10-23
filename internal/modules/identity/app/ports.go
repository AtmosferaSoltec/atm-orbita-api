// Package app son los casos de uso de las cuentas y sesiones, y los puertos
// (interfaces) que necesitan. No conoce Gin ni Postgres.
package app

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
)

// Users guarda las cuentas.
type Users interface {
	// Create devuelve domain.ErrEmailTaken si el correo ya existe.
	Create(ctx context.Context, user domain.User) error
	// ByEmail y ByID devuelven domain.ErrNotFound si no existe.
	ByEmail(ctx context.Context, email string) (domain.User, error)
	ByID(ctx context.Context, id uuid.UUID) (domain.User, error)
	TouchLastLogin(ctx context.Context, id uuid.UUID) error
	SetPassword(ctx context.Context, id uuid.UUID, hash string) error
	// Delete borra al usuario y, por cascada, todos sus datos.
	Delete(ctx context.Context, id uuid.UUID) error
}

// Session es un dispositivo con sesión abierta.
type Session struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	UserAgent string
}

// RefreshToken es lo que se guarda de un token de renovación: solo su hash.
type RefreshToken struct {
	ID        uuid.UUID
	SessionID uuid.UUID
	Hash      []byte
	ExpiresAt time.Time
}

// LockedRefreshToken es un token de renovación bloqueado para usarlo.
type LockedRefreshToken struct {
	ID             uuid.UUID
	SessionID      uuid.UUID
	UserID         uuid.UUID
	ExpiresAt      time.Time
	Used           bool
	SessionRevoked bool
}

// Sessions guarda sesiones y tokens de renovación.
type Sessions interface {
	Create(ctx context.Context, session Session) error
	IsActive(ctx context.Context, sessionID, userID uuid.UUID) (bool, error)
	Revoke(ctx context.Context, sessionID uuid.UUID, reason string) error
	Touch(ctx context.Context, sessionID uuid.UUID) error
	CreateRefreshToken(ctx context.Context, token RefreshToken) error
	// LockRefreshToken bloquea la fila hasta el fin de la transacción; devuelve
	// domain.ErrNotFound si el hash no existe.
	LockRefreshToken(ctx context.Context, hash []byte) (LockedRefreshToken, error)
	MarkRefreshTokenUsed(ctx context.Context, id uuid.UUID) error
	// RevokeAll cierra todas las sesiones del usuario.
	RevokeAll(ctx context.Context, userID uuid.UUID, reason string) error
}

// PasswordHasher calcula y comprueba hashes de contraseña.
type PasswordHasher interface {
	Hash(password string) (string, error)
	Verify(password, hash string) (bool, error)
}

// AccessClaims es lo que lleva un access token.
type AccessClaims struct {
	UserID    uuid.UUID
	SessionID uuid.UUID
}

// AccessTokens firma y verifica access tokens.
type AccessTokens interface {
	Issue(claims AccessClaims, now time.Time) (string, error)
	// Verify devuelve auth.ErrUnauthorized si el token no sirve.
	Verify(token string, now time.Time) (AccessClaims, error)
}

// Transactor abre una transacción (platform/tx.Runner).
type Transactor interface {
	InTx(ctx context.Context, fn func(ctx context.Context) error) error
}

// NewUser es lo que reciben los ganchos de alta.
type NewUser struct {
	ID           uuid.UUID
	Email        string
	MainCurrency string
	Timezone     string
}

// RegistrationHook corre dentro de la transacción del registro: si falla, no
// queda nada. Los módulos lo usan para crear el perfil, las categorías iniciales
// y la cuenta "Efectivo".
type RegistrationHook func(ctx context.Context, user NewUser) error

// DeletionHook corre dentro de la transacción de eliminar la cuenta, antes de
// borrar al usuario: si falla, no se borra nada. Es para lo que la cascada no
// alcanza (archivos fuera de la base); hoy ningún módulo lo necesita.
type DeletionHook func(ctx context.Context, userID uuid.UUID) error
