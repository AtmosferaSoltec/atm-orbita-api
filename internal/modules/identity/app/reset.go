package app

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/ids"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// ResetCode es un código para recuperar la contraseña: solo su hash.
type ResetCode struct {
	ID        uuid.UUID
	UserID    uuid.UUID
	Hash      []byte
	ExpiresAt time.Time
	Attempts  int
	CreatedAt time.Time
}

// ResetCodes guarda los códigos de recuperación.
type ResetCodes interface {
	CountSince(ctx context.Context, userID uuid.UUID, since time.Time) (int64, error)
	// Replace deja sin efecto los pendientes del usuario y guarda el nuevo.
	Replace(ctx context.Context, code ResetCode) error
	// LockActive bloquea el código vigente; domain.ErrNotFound si no hay.
	LockActive(ctx context.Context, userID uuid.UUID) (ResetCode, error)
	AddAttempt(ctx context.Context, id uuid.UUID) error
	// Use lo marca usado si sigue vigente; ok es false si ya no servía.
	Use(ctx context.Context, id uuid.UUID, now time.Time) (ok bool, err error)
}

// ResetMailer envía los correos de la recuperación (integrations/mail). Envía
// en segundo plano: no hace esperar la respuesta.
type ResetMailer interface {
	SendResetCode(ctx context.Context, to, code string, validFor time.Duration) error
	SendPasswordChanged(ctx context.Context, to string) error
}

// revokedPasswordReset es el motivo con que se cierran las sesiones al cambiar la contraseña.
const revokedPasswordReset = "password_reset"

// RequestPasswordReset envía un código al correo si tiene cuenta. Responde lo
// mismo, y en el mismo tiempo, exista o no: el trabajo se hace en segundo plano.
func (s *Service) RequestPasswordReset(ctx context.Context, email string) error {
	email = domain.NormalizeEmail(email)
	var problems domain.ValidationError
	domain.ValidateEmail(email, &problems)
	if err := problems.OrNil(); err != nil {
		return err
	}
	background := context.WithoutCancel(ctx)
	s.deps.Background(func() {
		if err := s.issueResetCode(background, email); err != nil {
			logger.From(background).Error("password reset code", "error", err)
		}
	})
	return nil
}

func (s *Service) issueResetCode(ctx context.Context, email string) error {
	user, err := s.deps.Users.ByEmail(ctx, email)
	if errors.Is(err, domain.ErrNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	now := s.deps.Clock.Now()
	sent, err := s.deps.ResetCodes.CountSince(ctx, user.ID, now.Add(-domain.ResetCodeSendWindow))
	if err != nil || sent >= domain.ResetCodeMaxSends {
		return err
	}
	code, err := newResetCode()
	if err != nil {
		return err
	}
	err = s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		return s.deps.ResetCodes.Replace(ctx, ResetCode{
			ID: ids.New(), UserID: user.ID, Hash: s.hashResetCode(user.ID, code),
			ExpiresAt: now.Add(domain.ResetCodeTTL), CreatedAt: now,
		})
	})
	if err != nil {
		return err
	}
	return s.deps.Mailer.SendResetCode(ctx, user.Email, code, domain.ResetCodeTTL)
}

// VerifyResetCode dice si el código sirve, para pasar a la contraseña nueva. No
// lo consume, pero un fallo cuenta como intento.
func (s *Service) VerifyResetCode(ctx context.Context, email, code string) error {
	email = domain.NormalizeEmail(email)
	var problems domain.ValidationError
	domain.ValidateEmail(email, &problems)
	domain.ValidateResetCode(code, &problems)
	if err := problems.OrNil(); err != nil {
		return err
	}
	return s.checkResetCode(ctx, email, code, nil)
}

// ResetPassword cambia la contraseña con el código, lo marca usado y cierra
// todas las sesiones. No inicia sesión: la app vuelve a Iniciar sesión. Una
// contraseña nueva inválida no consume el código.
func (s *Service) ResetPassword(ctx context.Context, email, code, newPassword string) error {
	email = domain.NormalizeEmail(email)
	var problems domain.ValidationError
	domain.ValidateEmail(email, &problems)
	domain.ValidateResetCode(code, &problems)
	var passwordProblems domain.ValidationError
	domain.ValidateNewPassword(newPassword, &passwordProblems)
	for _, f := range passwordProblems.Fields {
		problems.Add("new_password", f.Code)
	}
	if err := problems.OrNil(); err != nil {
		return err
	}
	hash, err := s.deps.Hasher.Hash(newPassword)
	if err != nil {
		return err
	}
	var to string
	err = s.checkResetCode(ctx, email, code, func(ctx context.Context, user domain.User) error {
		if err := s.deps.Users.SetPassword(ctx, user.ID, hash); err != nil {
			return err
		}
		to = user.Email
		return s.deps.Sessions.RevokeAll(ctx, user.ID, revokedPasswordReset)
	})
	if err != nil {
		return err
	}
	// Para que el dueño se entere si no fue él.
	if err := s.deps.Mailer.SendPasswordChanged(context.WithoutCancel(ctx), to); err != nil {
		logger.From(ctx).Error("password changed email", "error", err)
	}
	return nil
}

// checkResetCode comprueba el código vigente del correo dentro de una
// transacción. Con use != nil lo consume y corre use en la misma transacción.
// Cualquier problema es el mismo domain.ErrResetCodeInvalid.
func (s *Service) checkResetCode(ctx context.Context, email, code string, use func(ctx context.Context, user domain.User) error) error {
	invalid := false
	err := s.deps.Tx.InTx(ctx, func(ctx context.Context) error {
		now := s.deps.Clock.Now()
		user, err := s.deps.Users.ByEmail(ctx, email)
		if errors.Is(err, domain.ErrNotFound) {
			invalid = true
			return nil
		}
		if err != nil {
			return err
		}
		current, err := s.deps.ResetCodes.LockActive(ctx, user.ID)
		if errors.Is(err, domain.ErrNotFound) {
			invalid = true
			return nil
		}
		if err != nil {
			return err
		}
		if !now.Before(current.ExpiresAt) || current.Attempts >= domain.ResetCodeMaxAttempts {
			invalid = true
			return nil
		}
		if !hmac.Equal(s.hashResetCode(user.ID, code), current.Hash) {
			// El intento fallido se guarda: la transacción termina bien.
			invalid = true
			return s.deps.ResetCodes.AddAttempt(ctx, current.ID)
		}
		if use == nil {
			return nil
		}
		ok, err := s.deps.ResetCodes.Use(ctx, current.ID, now)
		if err != nil {
			return err
		}
		if !ok {
			invalid = true
			return nil
		}
		return use(ctx, user)
	})
	if err != nil {
		return err
	}
	if invalid {
		return domain.ErrResetCodeInvalid
	}
	return nil
}

// hashResetCode es el HMAC-SHA256 del código, ligado al usuario.
func (s *Service) hashResetCode(userID uuid.UUID, code string) []byte {
	mac := hmac.New(sha256.New, s.resetKey)
	mac.Write(userID[:])
	mac.Write([]byte(code))
	return mac.Sum(nil)
}

// deriveResetKey saca del secreto del servidor una clave propia para los códigos.
func deriveResetKey(secret []byte) []byte {
	mac := hmac.New(sha256.New, secret)
	mac.Write([]byte("orbita/password-reset-codes/v1"))
	return mac.Sum(nil)
}

// newResetCode son 6 dígitos de un generador criptográfico.
func newResetCode() (string, error) {
	n, err := rand.Int(rand.Reader, big.NewInt(1_000_000))
	if err != nil {
		return "", fmt.Errorf("generate reset code: %w", err)
	}
	return fmt.Sprintf("%06d", n.Int64()), nil
}
