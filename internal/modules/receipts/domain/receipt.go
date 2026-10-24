// Package domain son las reglas de los recibos por honorarios: qué datos se
// aceptan, el tope anual y qué archivo se puede guardar con un recibo. Las
// mismas reglas que la web (atm-orbita-web, src/lib/domain/receipts.ts).
package domain

import (
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

// Límites de los campos (los de la tabla receipts).
const (
	SeriesMaxLength      = 4
	NumberMaxLength      = 8
	ClientNameMaxLength  = 200
	DocNumberMaxLength   = 15
	DescriptionMaxLength = 500
)

// Tipos de documento del cliente.
const (
	DocRUC   = "RUC"
	DocDNI   = "DNI"
	DocOther = "OTRO"
)

// Origen del recibo.
const (
	SourceXML    = "xml"
	SourceManual = "manual"
)

// DefaultYearlyLimit es el tope de un año que la persona no configuró.
var DefaultYearlyLimit = money.NewAmount(decimal.NewFromInt(50_000))

// Códigos de campo.
const (
	CodeSeriesRequired       = "SERIES_REQUIRED"
	CodeSeriesTooLong        = "SERIES_TOO_LONG"
	CodeNumberRequired       = "NUMBER_REQUIRED"
	CodeNumberInvalid        = "NUMBER_INVALID"
	CodeIssueDateRequired    = "ISSUE_DATE_REQUIRED"
	CodeDateInFuture         = "DATE_IN_FUTURE"
	CodeClientNameRequired   = "CLIENT_NAME_REQUIRED"
	CodeClientNameTooLong    = "CLIENT_NAME_TOO_LONG"
	CodeDocTypeInvalid       = "DOC_TYPE_INVALID"
	CodeRUCInvalid           = "RUC_INVALID"
	CodeDNIInvalid           = "DNI_INVALID"
	CodeDocNumberTooLong     = "DOC_NUMBER_TOO_LONG"
	CodeAmountInvalid        = "AMOUNT_INVALID"
	CodeWithholdingInvalid   = "WITHHOLDING_INVALID"
	CodeDescriptionTooLong   = "DESCRIPTION_TOO_LONG"
	CodeCollectedBeforeIssue = "COLLECTED_BEFORE_ISSUE"
	CodeSourceInvalid        = "SOURCE_INVALID"
	CodeYearInvalid          = "YEAR_INVALID"
	CodeInvalidFormat        = "INVALID_FORMAT"
)

// Errores del módulo.
var (
	ErrNotFound        = errors.New("not found")
	ErrIDTaken         = errors.New("id already used by another record")
	ErrAlreadyExists   = errors.New("receipt already registered")
	ErrXMLMismatch     = errors.New("xml belongs to another receipt")
	ErrFileInvalid     = errors.New("file is not acceptable")
	ErrStorageDisabled = errors.New("storage unavailable")
)

// FieldError es un campo con problema.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas, en el orden del formulario de la web.
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

// Client es a quién se le emitió el recibo.
type Client struct {
	Name      string
	DocType   string
	DocNumber string
}

// Receipt es un recibo por honorarios.
type Receipt struct {
	ID          uuid.UUID
	Series      string
	Number      string
	IssueDate   dates.LocalDate
	Client      Client
	Description string
	Gross       money.Amount
	Withholding money.Amount
	CollectedOn *dates.LocalDate
	Source      string
	Documents   []Document
	CreatedAt   time.Time
	UpdatedAt   time.Time
}

// Net es lo que se recibe: bruto − retención.
func (r Receipt) Net() money.Amount {
	return money.NewAmount(r.Gross.Decimal().Sub(r.Withholding.Decimal()))
}

// Code es la serie y el número: "E001-109".
func (r Receipt) Code() string { return Code(r.Series, r.Number) }

// Code arma "SERIE-NÚMERO" en mayúsculas, para comparar.
func Code(series, number string) string {
	return strings.ToUpper(series) + "-" + NormalizeNumber(number)
}

var (
	digitsPattern = regexp.MustCompile(`^[0-9]+$`)
	rucPattern    = regexp.MustCompile(`^[0-9]{11}$`)
	dniPattern    = regexp.MustCompile(`^[0-9]{8}$`)
)

// NormalizeNumber quita los ceros a la izquierda: "00058" es "58".
func NormalizeNumber(number string) string {
	trimmed := strings.TrimLeft(strings.TrimSpace(number), "0")
	if trimmed == "" && strings.TrimSpace(number) != "" {
		return "0"
	}
	return trimmed
}

// Normalize recorta los textos y deja serie en mayúsculas y el número sin ceros.
func (r *Receipt) Normalize() {
	r.Series = strings.ToUpper(strings.TrimSpace(r.Series))
	r.Number = NormalizeNumber(r.Number)
	r.Client.Name = strings.Join(strings.Fields(r.Client.Name), " ")
	r.Client.DocType = strings.ToUpper(strings.TrimSpace(r.Client.DocType))
	r.Client.DocNumber = strings.TrimSpace(r.Client.DocNumber)
	r.Description = strings.TrimSpace(r.Description)
}

// Validate revisa un recibo ya normalizado. today es "hoy" en la zona de la persona.
func (r Receipt) Validate(today dates.LocalDate) error {
	var problems ValidationError
	switch {
	case r.Series == "":
		problems.Add("series", CodeSeriesRequired)
	case utf8.RuneCountInString(r.Series) > SeriesMaxLength:
		problems.Add("series", CodeSeriesTooLong)
	}
	switch {
	case r.Number == "":
		problems.Add("number", CodeNumberRequired)
	case len(r.Number) > NumberMaxLength || !digitsPattern.MatchString(r.Number):
		problems.Add("number", CodeNumberInvalid)
	}
	switch {
	case r.IssueDate.IsZero():
		problems.Add("issue_date", CodeIssueDateRequired)
	case r.IssueDate.After(today):
		problems.Add("issue_date", CodeDateInFuture)
	}
	switch {
	case r.Client.Name == "":
		problems.Add("client.name", CodeClientNameRequired)
	case utf8.RuneCountInString(r.Client.Name) > ClientNameMaxLength:
		problems.Add("client.name", CodeClientNameTooLong)
	}
	switch r.Client.DocType {
	case DocRUC:
		if !rucPattern.MatchString(r.Client.DocNumber) {
			problems.Add("client.doc_number", CodeRUCInvalid)
		}
	case DocDNI:
		if !dniPattern.MatchString(r.Client.DocNumber) {
			problems.Add("client.doc_number", CodeDNIInvalid)
		}
	case DocOther:
		if utf8.RuneCountInString(r.Client.DocNumber) > DocNumberMaxLength {
			problems.Add("client.doc_number", CodeDocNumberTooLong)
		}
	default:
		problems.Add("client.doc_type", CodeDocTypeInvalid)
	}
	if !r.Gross.IsPositive() {
		problems.Add("gross", CodeAmountInvalid)
	} else if r.Withholding.Decimal().IsNegative() || !r.Withholding.Decimal().LessThan(r.Gross.Decimal()) {
		problems.Add("withholding", CodeWithholdingInvalid)
	}
	if utf8.RuneCountInString(r.Description) > DescriptionMaxLength {
		problems.Add("description", CodeDescriptionTooLong)
	}
	if r.CollectedOn != nil {
		switch {
		case r.CollectedOn.Before(r.IssueDate):
			problems.Add("collected_on", CodeCollectedBeforeIssue)
		case r.CollectedOn.After(today):
			problems.Add("collected_on", CodeDateInFuture)
		}
	}
	if r.Source != SourceXML && r.Source != SourceManual {
		problems.Add("source", CodeSourceInvalid)
	}
	return problems.OrNil()
}

// ValidYear acepta años razonables para un recibo.
func ValidYear(year int) bool { return year >= 2000 && year <= 2100 }
