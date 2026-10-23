package domain

import (
	"errors"
	"regexp"
	"time"
)

// Reglas de los códigos para recuperar la contraseña (plan 11).
const (
	// ResetCodeTTL es lo que dura un código.
	ResetCodeTTL = 15 * time.Minute
	// ResetCodeMaxAttempts: al quinto fallo el código deja de servir.
	ResetCodeMaxAttempts = 5
	// ResetCodeMaxSends son los envíos por usuario cada ResetCodeSendWindow.
	ResetCodeMaxSends   = 3
	ResetCodeSendWindow = 24 * time.Hour
)

// CodeResetCodeFormat: el código no son 6 dígitos.
const CodeResetCodeFormat = "CODE_INVALID"

// ErrResetCodeInvalid: código incorrecto, usado, vencido, sin intentos o de un
// correo que no existe. Siempre el mismo error, para no delatar qué correos hay.
var ErrResetCodeInvalid = errors.New("reset code invalid")

var resetCodePattern = regexp.MustCompile(`^[0-9]{6}$`)

// ValidateResetCode revisa que el código sean 6 dígitos.
func ValidateResetCode(code string, problems *ValidationError) {
	if !resetCodePattern.MatchString(code) {
		problems.Add("code", CodeResetCodeFormat)
	}
}
