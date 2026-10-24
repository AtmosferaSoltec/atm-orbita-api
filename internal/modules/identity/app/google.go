package app

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/currency"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

const GoogleChallengeTTL = 5 * time.Minute

// GoogleAccount contains only claims verified by the Google adapter.
type GoogleAccount struct {
	Subject string
	Email   string
	Name    string
}

type GoogleVerifier interface {
	Verify(ctx context.Context, credential, nonce string) (GoogleAccount, error)
}

type ExternalIdentities interface {
	ByGoogleSubject(ctx context.Context, subject string) (uuid.UUID, error)
	LinkGoogle(ctx context.Context, subject string, userID uuid.UUID) error
}

type GoogleChallenges interface {
	Create(ctx context.Context, hash []byte, expiresAt, now time.Time) error
	Consume(ctx context.Context, hash []byte, now time.Time) (bool, error)
}

type GoogleInput struct {
	Credential   string
	Nonce        string
	Password     string
	MainCurrency string
	Timezone     string
	UserAgent    string
	// Web opens a browser session (longer refresh, cookie); false, a native one.
	Web bool
}

func (s *Service) NewGoogleChallenge(ctx context.Context) (string, error) {
	if s.deps.Google == nil {
		return "", domain.ErrGoogleUnavailable
	}
	nonce, err := newRefreshToken()
	if err != nil {
		return "", err
	}
	now := s.deps.Clock.Now()
	err = s.deps.Challenges.Create(ctx, hashToken(nonce), now.Add(GoogleChallengeTTL), now)
	return nonce, err
}

// GoogleSignIn creates or links an identity and opens an ordinary Orbita session.
// The challenge is consumed in the same transaction, so failed linking can retry
// while a successful credential exchange can never be replayed.
func (s *Service) GoogleSignIn(ctx context.Context, in GoogleInput) (AuthResult, error) {
	if s.deps.Google == nil {
		return AuthResult{}, domain.ErrGoogleUnavailable
	}
	if in.Nonce == "" || len(in.Nonce) > 128 || in.Credential == "" || len(in.Credential) > 16384 {
		return AuthResult{}, domain.ErrGoogleInvalid
	}
	account, err := s.deps.Google.Verify(ctx, in.Credential, in.Nonce)
	if err != nil {
		return AuthResult{}, err
	}
	email := domain.NormalizeEmail(account.Email)
	var problems domain.ValidationError
	domain.ValidateEmail(email, &problems)
	if account.Subject == "" || len(account.Subject) > 255 || problems.OrNil() != nil {
		return AuthResult{}, domain.ErrGoogleInvalid
	}
	now := s.deps.Clock.Now()
	var result AuthResult
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		consumed, err := s.deps.Challenges.Consume(ctx, hashToken(in.Nonce), now)
		if err != nil {
			return err
		}
		if !consumed {
			return domain.ErrGoogleChallengeExpired
		}

		id, lookupErr := s.deps.Identities.ByGoogleSubject(ctx, account.Subject)
		var user domain.User
		switch {
		case lookupErr == nil:
			user, err = s.deps.Users.ByID(ctx, id)
			if err != nil {
				return err
			}
		case errors.Is(lookupErr, domain.ErrNotFound):
			user, err = s.deps.Users.ByEmail(ctx, email)
			if err == nil {
				if err := s.confirmGoogleLink(ctx, user, in.Password, now); err != nil {
					return err
				}
			} else if errors.Is(err, domain.ErrNotFound) {
				mainCurrency := in.MainCurrency
				if mainCurrency == "" {
					mainCurrency = currency.Default
				}
				if !currency.IsSupported(mainCurrency) {
					problems.Add("main_currency", domain.CodeCurrencyNotSupported)
					return problems.OrNil()
				}
				// Google profile names can exceed the local display-name limit.
				name := domain.NormalizeName(truncate(account.Name, domain.NameMaxLength))
				user = domain.User{ID: ids.New(), Email: email, Name: name, CreatedAt: now}
				if err := s.deps.Users.Create(ctx, user); err != nil {
					return err
				}
				newUser := NewUser{ID: user.ID, Email: email, MainCurrency: mainCurrency, Timezone: validTimezone(in.Timezone)}
				for _, hook := range s.hooks {
					if err := hook(ctx, newUser); err != nil {
						return fmt.Errorf("registration hook: %w", err)
					}
				}
			} else {
				return err
			}
			if err := s.deps.Identities.LinkGoogle(ctx, account.Subject, user.ID); err != nil {
				return err
			}
		default:
			return lookupErr
		}
		if err := s.deps.Users.TouchLastLogin(ctx, user.ID); err != nil {
			return err
		}
		tokens, err := s.openSession(ctx, user.ID, in.UserAgent, now, in.Web)
		result = AuthResult{User: user, Tokens: tokens}
		return err
	})
	if err != nil {
		return AuthResult{}, err
	}
	logger.From(ctx).Info("security: Google login succeeded", "user_id", result.User.ID)
	return result, nil
}

func (s *Service) confirmGoogleLink(ctx context.Context, user domain.User, password string, now time.Time) error {
	if wait := s.attempts.wait(user.Email, now); wait > 0 {
		return &domain.TooManyAttemptsError{RetryAfter: wait}
	}
	if password == "" {
		return domain.ErrGoogleLinkRequired
	}
	if len(password) > domain.PasswordMaxLength*4 {
		return s.loginFailed(ctx, user.Email, now)
	}
	hash := user.PasswordHash
	if hash == "" {
		hash = s.dummyHash
	}
	ok, err := s.deps.Hasher.Verify(password, hash)
	if err != nil {
		return fmt.Errorf("verify linking password: %w", err)
	}
	if !ok || user.PasswordHash == "" {
		return s.loginFailed(ctx, user.Email, now)
	}
	s.attempts.succeeded(user.Email)
	return nil
}
