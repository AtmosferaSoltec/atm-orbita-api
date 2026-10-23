package app

import (
	"context"
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
	ok, err := s.deps.Hasher.Verify(password, user.PasswordHash)
	if err != nil {
		return fmt.Errorf("verify password: %w", err)
	}
	if !ok {
		s.attempts.failed(user.Email, now)
		logger.From(ctx).Warn("security: account deletion with a wrong password", "user_id", userID)
		return domain.ErrInvalidCredentials
	}
	s.attempts.succeeded(user.Email)

	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		for _, hook := range s.deletionHooks {
			if err := hook(ctx, userID); err != nil {
				return fmt.Errorf("deletion hook: %w", err)
			}
		}
		return s.deps.Users.Delete(ctx, userID)
	})
	if err != nil {
		return err
	}
	// Sin el correo: la cuenta ya no existe.
	logger.From(ctx).Info("security: account deleted", "user_id", userID)
	return nil
}
