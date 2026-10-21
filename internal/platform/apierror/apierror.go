// Package apierror da la forma única de las respuestas del API (AGENT_GUIDE §4):
//
//	éxito:    { "data": ... }
//	paginado: { "data": [...], "meta": { "page", "page_size", "total" } }
//	error:    { "error": { "code", "message", "details" } }
//
// El code es estable y en inglés: la app elige su texto a partir de él. El
// message va en español, como respaldo.
package apierror

import (
	"errors"
	"fmt"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/logger"
)

// Códigos comunes a todos los módulos. Cada módulo define los suyos.
const (
	CodeValidationFailed    = "VALIDATION_FAILED"
	CodeUnauthorized        = "UNAUTHORIZED"
	CodeNotFound            = "NOT_FOUND"
	CodeMethodNotAllowed    = "METHOD_NOT_ALLOWED"
	CodeConflict            = "CONFLICT"
	CodeIDConflict          = "ID_CONFLICT"
	CodePayloadTooLarge     = "PAYLOAD_TOO_LARGE"
	CodeTooManyRequests     = "TOO_MANY_REQUESTS"
	CodeInternal            = "INTERNAL_ERROR"
	CodeDatabaseUnavailable = "DATABASE_UNAVAILABLE"
)

// Error es un error que el API sabe contar a la app.
type Error struct {
	Status  int
	Code    string
	Message string
	Details any
}

func (e *Error) Error() string { return fmt.Sprintf("%s: %s", e.Code, e.Message) }

// New crea un error con su estado HTTP, su code y su mensaje en español.
func New(status int, code, message string) *Error {
	return &Error{Status: status, Code: code, Message: message}
}

// FieldError es un problema de un campo. Code son los de ValidationError de
// Android (AMOUNT_REQUIRED, FUTURE_DATE...), en su orden de revisión.
type FieldError struct {
	Field string `json:"field"`
	Code  string `json:"code"`
}

// Validation es un 400 con los problemas de cada campo; la app muestra el primero.
func Validation(fields ...FieldError) *Error {
	return &Error{
		Status:  http.StatusBadRequest,
		Code:    CodeValidationFailed,
		Message: "Revisa los datos enviados.",
		Details: fields,
	}
}

// Errores comunes ya armados.
var (
	ErrNotFound         = New(http.StatusNotFound, CodeNotFound, "Este registro ya no existe.")
	ErrUnauthorized     = New(http.StatusUnauthorized, CodeUnauthorized, "Tu sesión terminó. Inicia sesión de nuevo.")
	ErrMethodNotAllowed = New(http.StatusMethodNotAllowed, CodeMethodNotAllowed, "Esta operación no existe.")
	ErrTooManyRequests  = New(http.StatusTooManyRequests, CodeTooManyRequests, "Demasiados intentos. Espera un momento.")
	ErrPayloadTooLarge  = New(http.StatusRequestEntityTooLarge, CodePayloadTooLarge, "Los datos enviados son demasiado grandes.")
	ErrIDConflict       = New(http.StatusConflict, CodeIDConflict, "Ya existe un registro con ese identificador.")
	ErrInternal         = New(http.StatusInternalServerError, CodeInternal, "Algo salió mal. Inténtalo de nuevo.")
	ErrDatabaseDown     = New(http.StatusServiceUnavailable, CodeDatabaseUnavailable, "El servicio no está disponible. Inténtalo en un momento.")
)

type body struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Details any    `json:"details,omitempty"`
}

// Respond responde con err. Un error que no es *Error no se cuenta: se registra
// completo y la app recibe solo INTERNAL_ERROR (una traza puede llevar datos).
func Respond(c *gin.Context, err error) {
	var apiErr *Error
	var tooLarge *http.MaxBytesError
	switch {
	case errors.As(err, &apiErr):
	case errors.As(err, &tooLarge):
		apiErr = ErrPayloadTooLarge
	default:
		logger.From(c.Request.Context()).Error("unhandled error", "error", err)
		apiErr = ErrInternal
	}
	c.AbortWithStatusJSON(apiErr.Status, gin.H{"error": body{
		Code:    apiErr.Code,
		Message: apiErr.Message,
		Details: apiErr.Details,
	}})
}

// OK responde 200 con { "data": data }.
func OK(c *gin.Context, data any) {
	c.JSON(http.StatusOK, gin.H{"data": data})
}

// Created responde 201 con { "data": data }.
func Created(c *gin.Context, data any) {
	c.JSON(http.StatusCreated, gin.H{"data": data})
}

// NoContent responde 204 sin cuerpo.
func NoContent(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

// Meta es la paginación de una lista.
type Meta struct {
	Page     int   `json:"page"`
	PageSize int   `json:"page_size"`
	Total    int64 `json:"total"`
}

// Page responde 200 con { "data": items, "meta": meta }.
func Page(c *gin.Context, items any, meta Meta) {
	c.JSON(http.StatusOK, gin.H{"data": items, "meta": meta})
}
