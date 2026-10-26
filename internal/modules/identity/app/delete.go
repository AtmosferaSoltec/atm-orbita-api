package app

import (
	"context"
	"errors"
	"fmt"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// OnUserDeleted agrega un gancho a la eliminación de la cuenta.
func (s *Service) OnUserDeleted(hook DeletionHook) {
	s.deletionHooks = append(s.deletionHooks, hook)
}

// DeleteAccount borra de verdad la cuenta y todos sus datos (plan 12). Pide la
// contraseña aunque el token sea válido: un teléfono desbloqueado en manos
// ajenas no basta. Los fallos cuentan como los del inicio de sesión.
func (s *Service) DeleteAccount(ctx context.Context, userID uuid.UUID, password string) error {
	if password == "" {
		var problems domain.ValidationError
		problems.Add("password", domain.CodePasswordRequired)
		return problems.OrNil()
	}
	user, err := s.deps.Users.ByID(ctx, userID)
	if err != nil {
		return err
	}
	now := s.deps.Clock.Now()
	if wait := s.attempts.wait(user.Email, now); wait > 0 {
		return &domain.TooManyAttemptsError{RetryAfter: wait}
	}
	hash := user.PasswordHash
	if hash == "" {
		hash = s.dummyHash
	}
	ok, err := s.deps.Hasher.Verify(password, hash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok || user.PasswordHash == "" {
		s.attempts.failed(user.Email, now)
		logger.From(ctx).Warn("security: account deletion with a wrong password", "user_id", userID)
		return domain.ErrInvalidCredentials
	}
	s.attempts.succeeded(user.Email)

	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error { return s.removeAccount(ctx, userID) })
	if err != nil {
		return err
	}
	// Sin el correo: la cuenta ya no existe.
	logger.From(ctx).Info("security: account deleted", "user_id", userID)
	return nil
}

// DeleteAccountWithGoogle es DeleteAccount para quien entra con Google y no tiene
// contraseña (plan 21). No pide sesión: la credencial de Google, recién emitida y
// con un reto de un solo uso, ya prueba quién es, igual que al iniciar sesión, y
// así sirve también a quien ya desinstaló la app (Google Play lo exige). Se borra
// la cuenta vinculada a esa identidad de Google; una identidad sin cuenta no borra
// nada. Si algo falla, el reto se devuelve (todo va en una transacción).
func (s *Service) DeleteAccountWithGoogle(ctx context.Context, credential, nonce string) error {
	if s.deps.Google == nil {
		return domain.ErrGoogleUnavailable
	}
	if nonce == "" || len(nonce) > 128 || credential == "" || len(credential) > 16384 {
		return domain.ErrGoogleInvalid
	}
	account, err := s.deps.Google.Verify(ctx, credential, nonce)
	if err != nil {
		return err
	}
	now := s.deps.Clock.Now()
	var userID uuid.UUID
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		consumed, err := s.deps.Challenges.Consume(ctx, hashToken(nonce), now)
		if err != nil {
			return err
		}
		if !consumed {
			return domain.ErrGoogleChallengeExpired
		}
		userID, err = s.deps.Identities.ByGoogleSubject(ctx, account.Subject)
		if errors.Is(err, domain.ErrNotFound) {
			return domain.ErrGoogleNotLinked
		}
		if err != nil {
			return err
		}
		return s.removeAccount(ctx, userID)
	})
	if err != nil {
		return err
	}
	logger.From(ctx).Info("security: account deleted with Google", "user_id", userID)
	return nil
}

// removeAccount corre los ganchos y borra al usuario; quien llama ya abrió la transacción.
func (s *Service) removeAccount(ctx context.Context, userID uuid.UUID) error {
	for _, hook := range s.deletionHooks {
		if err := hook(ctx, userID); err != nil {
			return fmt.Errorf("deletion hook: %w", err)
		}
	}
	return s.deps.Users.Delete(ctx, userID)
}
