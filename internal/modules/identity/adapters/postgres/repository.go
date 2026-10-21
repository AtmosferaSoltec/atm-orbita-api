// Package postgres implementa los puertos de guardado del módulo identity con
// las consultas de queries.sql (generadas por sqlc en sqlcgen/).
package postgres

import (
	"context"
	"errors"

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
		ID: user.ID, Email: user.Email, PasswordHash: user.PasswordHash,
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
	return domain.User{ID: row.ID, Email: row.Email, PasswordHash: row.PasswordHash, CreatedAt: row.CreatedAt}, nil
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
	return domain.User{ID: row.ID, Email: row.Email, PasswordHash: row.PasswordHash, CreatedAt: row.CreatedAt}, nil
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
