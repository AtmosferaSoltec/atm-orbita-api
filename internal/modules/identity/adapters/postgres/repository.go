// Package postgres implementa los puertos de guardado del módulo identity con
// las consultas de queries.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/tx"
)

const uniqueViolation = "23505"

// DB da la conexión de la transacción en curso, o el pool (tx.Runner).
type DB interface {
	DB(ctx context.Context) tx.DBTX
}

// Users implementa app.Users.
type Users struct {
	db DB
}

// NewUsers crea el repositorio de cuentas.
func NewUsers(db DB) Users { return Users{db: db} }

func (r Users) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// Create guarda la cuenta; el índice único sobre email decide si ya existe.
func (r Users) Create(ctx context.Context, user domain.User) error {
	err := r.q(ctx).CreateUser(ctx, sqlcgen.CreateUserParams{
		ID: user.ID, Email: user.Email, PasswordHash: user.PasswordHash, Name: user.Name,
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrEmailTaken
	}
	return err
}

// ByEmail busca una cuenta por su correo ya normalizado.
func (r Users) ByEmail(ctx context.Context, email string) (domain.User, error) {
	row, err := r.q(ctx).GetUserByEmail(ctx, email)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return domain.User{ID: row.ID, Email: row.Email, Name: row.Name, PasswordHash: row.PasswordHash, CreatedAt: row.CreatedAt}, nil
}

// ByID busca una cuenta por su id.
func (r Users) ByID(ctx context.Context, id uuid.UUID) (domain.User, error) {
	row, err := r.q(ctx).GetUserByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.User{}, domain.ErrNotFound
	}
	if err != nil {
		return domain.User{}, err
	}
	return domain.User{ID: row.ID, Email: row.Email, Name: row.Name, PasswordHash: row.PasswordHash, CreatedAt: row.CreatedAt}, nil
}

// TouchLastLogin registra el último inicio de sesión.
func (r Users) TouchLastLogin(ctx context.Context, id uuid.UUID) error {
	return r.q(ctx).TouchLastLogin(ctx, id)
}

// Sessions implementa app.Sessions.
type Sessions struct {
	db DB
}

// NewSessions crea el repositorio de sesiones.
func NewSessions(db DB) Sessions { return Sessions{db: db} }

func (r Sessions) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// Create abre una sesión.
func (r Sessions) Create(ctx context.Context, session app.Session) error {
	var userAgent *string
	if session.UserAgent != "" {
		userAgent = &session.UserAgent
	}
	return r.q(ctx).CreateSession(ctx, sqlcgen.CreateSessionParams{
		ID: session.ID, UserID: session.UserID, UserAgent: userAgent,
	})
}

// IsActive dice si la sesión existe, es de ese usuario y no está revocada.
func (r Sessions) IsActive(ctx context.Context, sessionID, userID uuid.UUID) (bool, error) {
	return r.q(ctx).IsSessionActive(ctx, sqlcgen.IsSessionActiveParams{ID: sessionID, UserID: userID})
}

// Revoke cierra la sesión con su motivo.
func (r Sessions) Revoke(ctx context.Context, sessionID uuid.UUID, reason string) error {
	return r.q(ctx).RevokeSession(ctx, sqlcgen.RevokeSessionParams{ID: sessionID, RevokedReason: &reason})
}

// Touch registra el último uso.
func (r Sessions) Touch(ctx context.Context, sessionID uuid.UUID) error {
	return r.q(ctx).TouchSession(ctx, sessionID)
}

// CreateRefreshToken guarda el hash de un token de renovación.
func (r Sessions) CreateRefreshToken(ctx context.Context, token app.RefreshToken) error {
	return r.q(ctx).CreateRefreshToken(ctx, sqlcgen.CreateRefreshTokenParams{
		ID: token.ID, SessionID: token.SessionID, TokenHash: token.Hash, ExpiresAt: token.ExpiresAt,
	})
}

// LockRefreshToken bloquea el token hasta el fin de la transacción.
func (r Sessions) LockRefreshToken(ctx context.Context, hash []byte) (app.LockedRefreshToken, error) {
	row, err := r.q(ctx).LockRefreshToken(ctx, hash)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.LockedRefreshToken{}, domain.ErrNotFound
	}
	if err != nil {
		return app.LockedRefreshToken{}, err
	}
	return app.LockedRefreshToken{
		ID:             row.ID,
		SessionID:      row.SessionID,
		UserID:         row.UserID,
		ExpiresAt:      row.ExpiresAt,
		Used:           row.UsedAt != nil,
		SessionRevoked: row.SessionRevokedAt != nil,
	}, nil
}

// MarkRefreshTokenUsed marca el token como usado (rotación).
func (r Sessions) MarkRefreshTokenUsed(ctx context.Context, id uuid.UUID) error {
	return r.q(ctx).MarkRefreshTokenUsed(ctx, id)
}

// ResetCodes implementa app.ResetCodes.
type ResetCodes struct {
	db DB
}

// NewResetCodes crea el repositorio.
func NewResetCodes(db DB) ResetCodes { return ResetCodes{db: db} }

func (r ResetCodes) q(ctx context.Context) *sqlcgen.Queries { return sqlcgen.New(r.db.DB(ctx)) }

// CountSince cuenta los códigos pedidos por el usuario desde since.
func (r ResetCodes) CountSince(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error) {
	return r.q(ctx).CountResetCodesSince(ctx, sqlcgen.CountResetCodesSinceParams{UserID: userID, CreatedAt: since})
}

// Replace deja sin efecto los códigos pendientes del usuario y guarda el nuevo.
func (r ResetCodes) Replace(ctx context.Context, code app.ResetCode) error {
	q := r.q(ctx)
	if err := q.DiscardResetCodes(ctx, sqlcgen.DiscardResetCodesParams{UserID: code.UserID, UsedAt: &code.CreatedAt}); err != nil {
		return err
	}
	return q.CreateResetCode(ctx, sqlcgen.CreateResetCodeParams{
		ID: code.ID, UserID: code.UserID, CodeHash: code.Hash, ExpiresAt: code.ExpiresAt, CreatedAt: code.CreatedAt,
	})
}

// LockActive bloquea el código vigente del usuario; domain.ErrNotFound si no hay.
func (r ResetCodes) LockActive(ctx context.Context, userID uuid.UUID) (app.ResetCode, error) {
	row, err := r.q(ctx).LockActiveResetCode(ctx, userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return app.ResetCode{}, domain.ErrNotFound
	}
	if err != nil {
		return app.ResetCode{}, err
	}
	return app.ResetCode{ID: row.ID, UserID: userID, Hash: row.CodeHash, ExpiresAt: row.ExpiresAt, Attempts: int(row.Attempts)}, nil
}

// AddAttempt suma un intento fallido.
func (r ResetCodes) AddAttempt(ctx context.Context, id uuid.UUID) error {
	return r.q(ctx).AddResetCodeAttempt(ctx, id)
}

// Use marca el código usado si sigue vigente; ok es false si ya no servía.
func (r ResetCodes) Use(ctx context.Context, id uuid.UUID, now time.Time) (bool, error) {
	n, err := r.q(ctx).UseResetCode(ctx, sqlcgen.UseResetCodeParams{ID: id, Now: &now})
	return n == 1, err
}

// SetPassword guarda el hash de la contraseña nueva.
func (r Users) SetPassword(ctx context.Context, id uuid.UUID, hash string) error {
	return sqlcgen.New(r.db.DB(ctx)).UpdatePassword(ctx, sqlcgen.UpdatePasswordParams{ID: id, PasswordHash: hash})
}

// RevokeAll cierra todas las sesiones del usuario.
func (r Sessions) RevokeAll(ctx context.Context, userID uuid.UUID, reason string) error {
	return sqlcgen.New(r.db.DB(ctx)).RevokeUserSessions(ctx, sqlcgen.RevokeUserSessionsParams{UserID: userID, RevokedReason: &reason})
}

// Delete borra al usuario; la cascada se lleva el resto. domain.ErrNotFound si
// ya no existía.
func (r Users) Delete(ctx context.Context, id uuid.UUID) error {
	n, err := r.q(ctx).DeleteUser(ctx, id)
	if err != nil {
		return err
	}
	if n == 0 {
		return domain.ErrNotFound
	}
	return nil
}
