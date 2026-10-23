// Package domain son las reglas de las cuentas de usuario: qué es un correo
// válido, qué contraseña se acepta y qué errores puede dar la autenticación.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
)

// Longitudes de contraseña. El mínimo es el de Android; el máximo evita que una
// contraseña gigante agote la CPU al calcular su hash.
const (
	PasswordMinLength = 8
	PasswordMaxLength = 128
	EmailMaxLength    = 254
)

// El mismo patrón que validateEmail de Android (domain/model/Drafts.kt).
var emailPattern = regexp.MustCompile(`^[^@\s]+@[^@\s]+\.[^@\s]+$`)

// Códigos de campo, los mismos del enum ValidationError de Android.
const (
	CodeEmailInvalid         = "EMAIL_INVALID"
	CodePasswordTooShort     = "PASSWORD_TOO_SHORT"
	CodePasswordTooLong      = "PASSWORD_TOO_LONG"
	CodePasswordRequired     = "PASSWORD_REQUIRED"
	CodeCurrencyNotSupported = "CURRENCY_NOT_SUPPORTED"
	CodeInvalidFormat        = "INVALID_FORMAT"
)

// Errores de la autenticación.
var (
	ErrEmailTaken         = errors.New("email already registered")
	ErrInvalidCredentials = errors.New("invalid credentials")
	ErrSessionExpired     = errors.New("session expired")
	ErrNotFound           = errors.New("not found")
)

// TooManyAttemptsError: demasiados fallos seguidos con un mismo correo.
type TooManyAttemptsError struct {
	RetryAfter time.Duration
}

func (e *TooManyAttemptsError) Error() string {
	return fmt.Sprintf("too many attempts, retry after %s", e.RetryAfter)
}

// FieldError es un campo con problema, con el código que la app sabe mostrar.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas de un formulario, en el orden de
// revisión de Android: la app muestra el primero.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("validation failed: %v", e.Fields)
}

// Add agrega un problema.
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldError{Field: field, Code: code})
}

// OrNil devuelve el error solo si hay problemas.
func (e *ValidationError) OrNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}

// User es una cuenta de usuario.
type User struct {
	ID           uuid.UUID
	Email        string
	PasswordHash string
	CreatedAt    time.Time
}

// NormalizeEmail recorta y pasa a minúsculas: "Ana@Correo.com " es ana@correo.com.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail recibe el correo ya normalizado.
func ValidateEmail(email string, problems *ValidationError) {
	if len(email) > EmailMaxLength || !emailPattern.MatchString(email) {
		problems.Add("email", CodeEmailInvalid)
	}
}

// ValidateNewPassword es para una contraseña que se está eligiendo. No se
// imponen reglas de composición: solo el largo.
func ValidateNewPassword(password string, problems *ValidationError) {
	switch length := utf8.RuneCountInString(password); {
	case length < PasswordMinLength:
		problems.Add("password", CodePasswordTooShort)
	case length > PasswordMaxLength:
		problems.Add("password", CodePasswordTooLong)
	}
}
