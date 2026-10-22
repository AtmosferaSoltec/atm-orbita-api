// Package domain son las reglas del libro del usuario: categorías, cuentas,
// movimientos y transferencias. Go puro, sin Gin ni Postgres.
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"unicode/utf8"

	"github.com/google/uuid"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/text"
)

// Kind es el tipo de una categoría: ingreso o egreso.
type Kind string

// Tipos de categoría.
const (
	KindIncome  Kind = "income"
	KindExpense Kind = "expense"
)

// NameMaxLength es el largo máximo de un nombre (cuenta, categoría, tarjeta).
const NameMaxLength = 60

var colorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)

// Códigos de campo, los del enum ValidationError de Android cuando existen.
const (
	CodeNameRequired  = "NAME_REQUIRED"
	CodeNameTooLong   = "NAME_TOO_LONG"
	CodeColorInvalid  = "COLOR_INVALID"
	CodeKindInvalid   = "KIND_INVALID"
	CodeInvalidFormat = "INVALID_FORMAT"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrNameTaken = errors.New("category name taken")
	ErrIDTaken   = errors.New("id already used with other content")
)

// FieldError es un campo con problema.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas de un formulario, en el orden de Android.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return fmt.Sprintf("validation failed: %v", e.Fields) }

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

// Category clasifica movimientos. Su tipo no cambia después de crearla.
type Category struct {
	ID    uuid.UUID
	Name  string
	Kind  Kind
	Color string
}

// NameKey es la clave con que se compara el nombre: sin mayúsculas ni tildes.
func (c Category) NameKey() string { return text.NormalizeName(c.Name) }

// SameContent dice si otra categoría es la misma, para saber si un POST con un id
// ya usado es un reintento de la app o un choque.
func (c Category) SameContent(other Category) bool {
	return c.Name == other.Name && c.Kind == other.Kind && c.Color == other.Color
}

// ValidateName limpia el nombre y revisa su largo (en caracteres, no bytes).
func ValidateName(name string, problems *ValidationError) string {
	clean := text.CleanName(name)
	switch {
	case clean == "":
		problems.Add("name", CodeNameRequired)
	case utf8.RuneCountInString(clean) > NameMaxLength:
		problems.Add("name", CodeNameTooLong)
	}
	return clean
}

// ValidateColor revisa el formato #RRGGBB.
func ValidateColor(color string, problems *ValidationError) {
	if !colorPattern.MatchString(color) {
		problems.Add("color", CodeColorInvalid)
	}
}

// ValidateKind revisa que sea ingreso o egreso.
func ValidateKind(kind Kind, problems *ValidationError) {
	if kind != KindIncome && kind != KindExpense {
		problems.Add("kind", CodeKindInvalid)
	}
}

// InitialCategory es una de las categorías con que nace un usuario.
type InitialCategory struct {
	Name  string
	Kind  Kind
	Color string
}

// InitialCategories son las 9 con que nace todo usuario, en este orden (el de
// SampleData.kt en Android).
var InitialCategories = []InitialCategory{
	{"Alimentación", KindExpense, "#0E7490"},
	{"Transporte", KindExpense, "#B45309"},
	{"Vivienda", KindExpense, "#1D4ED8"},
	{"Salud", KindExpense, "#7C3AED"},
	{"Ocio", KindExpense, "#BE185D"},
	{"Otros", KindExpense, "#64748B"},
	{"Sueldo", KindIncome, "#0B7A5A"},
	{"Freelance", KindIncome, "#0E7490"},
	{"Otros ingresos", KindIncome, "#64748B"},
}
