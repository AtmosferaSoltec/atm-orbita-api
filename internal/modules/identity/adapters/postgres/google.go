package postgres

import (
	"context"
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/adapters/postgres/sqlcgen"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
)

type GoogleStore struct{ db DB }

func NewGoogleStore(db DB) GoogleStore { return GoogleStore{db: db} }

func (r GoogleStore) ByGoogleSubject(ctx context.Context, subject string) (uuid.UUID, error) {
	id, err := sqlcgen.New(r.db.DB(ctx)).GetExternalIdentityUser(ctx, subject)
	if errors.Is(err, pgx.ErrNoRows) {
		return uuid.Nil, domain.ErrNotFound
	}
	return id, err
}

func (r GoogleStore) LinkGoogle(ctx context.Context, subject string, userID uuid.UUID) error {
	err := sqlcgen.New(r.db.DB(ctx)).CreateExternalIdentity(ctx, sqlcgen.CreateExternalIdentityParams{Subject: subject, UserID: userID})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == uniqueViolation {
		return domain.ErrGoogleIdentityConflict
	}
	return err
}

func (r GoogleStore) Create(ctx context.Context, hash []byte, expiresAt, now time.Time) error {
	q := sqlcgen.New(r.db.DB(ctx))
	if err := q.DeleteExpiredGoogleChallenges(ctx, now); err != nil {
		return err
	}
	return q.CreateGoogleChallenge(ctx, sqlcgen.CreateGoogleChallengeParams{NonceHash: hash, ExpiresAt: expiresAt})
}

func (r GoogleStore) Consume(ctx context.Context, hash []byte, now time.Time) (bool, error) {
	n, err := sqlcgen.New(r.db.DB(ctx)).ConsumeGoogleChallenge(ctx, sqlcgen.ConsumeGoogleChallengeParams{NonceHash: hash, ExpiresAt: now})
	return n == 1, err
}
