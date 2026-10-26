// Package domain defines interest-free debtor ledgers with manually planned months.
package domain

import (
	"errors"
	"fmt"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"sort"
	"strings"
	"time"
	"unicode/utf8"
)

var (
	ErrNotFound       = errors.New("debtor or movement not found")
	ErrIDConflict     = errors.New("id belongs to another record")
	ErrCurrencyLocked = errors.New("debtor currency is immutable")
	ErrKindLocked     = errors.New("movement kind is immutable")
	ErrBalance        = errors.New("payments exceed advances in date order")
	maxAmount         = decimal.RequireFromString("999999999999.99")
)

type FieldError struct {
	Field string
	Code  string
}
type ValidationError struct{ Fields []FieldError }

func (e *ValidationError) Error() string { return fmt.Sprintf("validation failed: %v", e.Fields) }
func (e *ValidationError) Add(field, code string) {
	e.Fields = append(e.Fields, FieldError{field, code})
}
func (e *ValidationError) OrNil() error {
	if len(e.Fields) == 0 {
		return nil
	}
	return e
}
func Invalid(field, code string) error { return &ValidationError{Fields: []FieldError{{field, code}}} }

type Plan struct {
	Month  string
	Amount money.Amount
}
type Movement struct {
	ID           uuid.UUID
	Kind         string
	Amount       money.Amount
	Date         dates.LocalDate
	AppliedMonth string
	Title        string
	Note         string
	CreatedAt    time.Time
}
type Debtor struct {
	ID         uuid.UUID
	Name       string
	Currency   string
	ShareToken *string
	Movements  []Movement
	Plans      []Plan
	CreatedAt  time.Time
}

func (d *Debtor) Normalize() { d.Name = strings.TrimSpace(d.Name) }
func (d Debtor) Validate() error {
	var v ValidationError
	if d.Name == "" {
		v.Add("name", "DEBTOR_NAME_REQUIRED")
	} else if utf8.RuneCountInString(d.Name) > 60 {
		v.Add("name", "NAME_TOO_LONG")
	}
	if d.Currency != "PEN" && d.Currency != "USD" {
		v.Add("currency", "CURRENCY_NOT_SUPPORTED")
	}
	return v.OrNil()
}
func ValidMonth(month string) bool {
	if len(month) != 7 {
		return false
	}
	d, err := dates.Parse(month + "-01")
	return err == nil && validDate(d)
}
func validDate(d dates.LocalDate) bool {
	return !d.IsZero() && d.Time().Year() >= 2000 && d.Time().Year() <= 2100
}
func ValidatePlan(plan []Plan) error {
	if len(plan) > 1212 {
		return Invalid("plan", "LOAN_PLAN_INVALID")
	}
	total := decimal.Zero
	seen := map[string]bool{}
	for _, p := range plan {
		if !ValidMonth(p.Month) || seen[p.Month] {
			return Invalid("plan.month", "LOAN_PLAN_MONTH_INVALID")
		}
		seen[p.Month] = true
		if !p.Amount.IsPositive() || p.Amount.Decimal().GreaterThan(maxAmount) {
			return Invalid("plan.amount", "AMOUNT_INVALID")
		}
		total = total.Add(p.Amount.Decimal())
	}
	if total.GreaterThan(maxAmount) {
		return Invalid("plan", "LOAN_TOTAL_TOO_LARGE")
	}
	return nil
}
func (m *Movement) Normalize() {
	m.Title = strings.TrimSpace(m.Title)
	m.Note = strings.TrimSpace(m.Note)
}
func (m Movement) Validate(today dates.LocalDate) error {
	if m.Kind != "loan" && m.Kind != "payment" {
		return Invalid("kind", "LOAN_KIND_INVALID")
	}
	if !m.Amount.IsPositive() || m.Amount.Decimal().GreaterThan(maxAmount) {
		return Invalid("amount", "AMOUNT_INVALID")
	}
	if !validDate(m.Date) || m.Date.After(today) {
		return Invalid("date", "LOAN_DATE_INVALID")
	}
	if utf8.RuneCountInString(m.Title) > 100 {
		return Invalid("title", "LOAN_TITLE_TOO_LONG")
	}
	if utf8.RuneCountInString(m.Note) > 500 {
		return Invalid("note", "DESCRIPTION_TOO_LONG")
	}
	if m.Kind == "payment" {
		if !ValidMonth(m.AppliedMonth) {
			return Invalid("applied_month", "LOAN_PAYMENT_MONTH_INVALID")
		}
	} else {
		if m.AppliedMonth != "" {
			return Invalid("applied_month", "LOAN_PAYMENT_MONTH_INVALID")
		}
	}
	return nil
}

// ValidateLedger operates on exact decimals and covers corrections and deletions, too.
func ValidateLedger(items []Movement) error {
	entries := append([]Movement(nil), items...)
	sort.SliceStable(entries, func(i, j int) bool {
		if entries[i].Date.String() != entries[j].Date.String() {
			return entries[i].Date.Before(entries[j].Date)
		}
		return entries[i].Kind == "loan" && entries[j].Kind == "payment"
	})
	lent, paid := decimal.Zero, decimal.Zero
	for _, m := range entries {
		if m.Kind == "loan" {
			lent = lent.Add(m.Amount.Decimal())
		} else {
			paid = paid.Add(m.Amount.Decimal())
		}
		if lent.GreaterThan(maxAmount) || paid.GreaterThan(maxAmount) {
			return Invalid("amount", "LOAN_TOTAL_TOO_LARGE")
		}
		if paid.GreaterThan(lent) {
			return ErrBalance
		}
	}
	return nil
}
func SameMovement(a, b Movement) bool {
	return a.Kind == b.Kind &&
		a.Amount.Decimal().Equal(b.Amount.Decimal()) &&
		a.Date.String() == b.Date.String() &&
		a.AppliedMonth == b.AppliedMonth &&
		a.Title == b.Title &&
		a.Note == b.Note
}
