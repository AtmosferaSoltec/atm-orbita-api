// Package domain defines monthly services and manually assigned member shares.
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

var (
	ErrNotFound       = errors.New("service not found")
	ErrIDConflict     = errors.New("id belongs to another record")
	ErrCurrencyLocked = errors.New("currency cannot change after recording payments")
	colorPattern      = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
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

type Member struct {
	ID     uuid.UUID
	Name   string
	Amount money.Amount
	Since  string
}
type Payment struct {
	ID       uuid.UUID
	MemberID uuid.UUID
	Month    string
	PaidOn   dates.LocalDate
	Amount   money.Amount
}
type Service struct {
	ID         uuid.UUID
	Name       string
	Color      string
	Currency   string
	Amount     money.Amount
	BillingDay int
	StartedOn  dates.LocalDate
	Status     string
	EndedOn    *dates.LocalDate
	Note       string
	Shared     bool
	Members    []Member
	Payments   []Payment
	CreatedAt  time.Time
}

func (s *Service) Normalize() {
	s.Name = strings.TrimSpace(s.Name)
	s.Note = strings.TrimSpace(s.Note)
	if s.Status != "cancelled" {
		s.EndedOn = nil
	}
	if !s.Shared {
		s.Members = []Member{}
	}
	for i := range s.Members {
		s.Members[i].Name = strings.TrimSpace(s.Members[i].Name)
	}
}

func (s Service) Validate() error {
	var v ValidationError
	if s.Name == "" {
		v.Add("name", "SERVICE_NAME_REQUIRED")
	} else if utf8.RuneCountInString(s.Name) > 60 {
		v.Add("name", "NAME_TOO_LONG")
	}
	if !colorPattern.MatchString(s.Color) {
		v.Add("color", "SERVICE_COLOR_INVALID")
	}
	if s.Currency != "PEN" && s.Currency != "USD" {
		v.Add("currency", "CURRENCY_NOT_SUPPORTED")
	}
	if !s.Amount.IsPositive() {
		v.Add("amount", "AMOUNT_INVALID")
	}
	if s.BillingDay < 1 || s.BillingDay > 31 {
		v.Add("billing_day", "BILLING_DAY_INVALID")
	}
	if !ValidDate(s.StartedOn) {
		v.Add("started_on", "SERVICE_DATE_INVALID")
	}
	if s.Status != "active" && s.Status != "paused" && s.Status != "cancelled" {
		v.Add("status", "SERVICE_STATUS_INVALID")
	}
	if s.Status == "cancelled" && (s.EndedOn == nil || !ValidDate(*s.EndedOn) || s.EndedOn.Before(s.StartedOn)) {
		v.Add("ended_on", "SERVICE_END_INVALID")
	}
	if s.Shared && (len(s.Members) == 0 || len(s.Members) > 100) {
		v.Add("members", "SERVICE_MEMBERS_REQUIRED")
	}
	total := decimal.Zero
	seen := map[uuid.UUID]bool{}
	for _, m := range s.Members {
		if m.ID == uuid.Nil || seen[m.ID] {
			v.Add("members", "SERVICE_MEMBER_INVALID")
		}
		seen[m.ID] = true
		if m.Name == "" || utf8.RuneCountInString(m.Name) > 60 {
			v.Add("members.name", "SERVICE_MEMBER_NAME_INVALID")
		}
		if !m.Amount.IsPositive() {
			v.Add("members.amount", "AMOUNT_INVALID")
		}
		if !ValidMonth(m.Since) || (!s.StartedOn.IsZero() && m.Since < s.StartedOn.String()[:7]) {
			v.Add("members.since", "SERVICE_MEMBER_MONTH_INVALID")
		}
		total = total.Add(m.Amount.Decimal())
	}
	if total.GreaterThan(s.Amount.Decimal()) {
		v.Add("members.amount", "SERVICE_SHARES_EXCEED_TOTAL")
	}
	if utf8.RuneCountInString(s.Note) > 500 {
		v.Add("note", "SERVICE_NOTE_TOO_LONG")
	}
	return v.OrNil()
}

func ValidDate(d dates.LocalDate) bool {
	return !d.IsZero() && d.Time().Year() >= 2000 && d.Time().Year() <= 2100
}

func ValidMonth(month string) bool {
	if len(month) != 7 {
		return false
	}
	d, err := dates.Parse(month + "-01")
	return err == nil && ValidDate(d)
}

func DueDate(month string, day int) dates.LocalDate {
	t, _ := time.Parse("2006-01", month)
	last := time.Date(t.Year(), t.Month()+1, 0, 0, 0, 0, 0, time.UTC).Day()
	if day > last {
		day = last
	}
	return dates.New(t.Year(), t.Month(), day)
}

// Payments use the member's server-side share, never a client-supplied amount.
func (s Service) PaymentAmount(memberID uuid.UUID, month string, paidOn, today dates.LocalDate) (money.Amount, error) {
	var v ValidationError
	if !ValidMonth(month) {
		v.Add("month", "SERVICE_PAYMENT_MONTH_INVALID")
		return money.Amount{}, v.OrNil()
	}
	if !ValidDate(paidOn) || paidOn.After(today) || paidOn.Before(s.StartedOn) {
		v.Add("paid_on", "SERVICE_PAYMENT_DATE_INVALID")
	}
	for _, m := range s.Members {
		if m.ID != memberID {
			continue
		}
		due := DueDate(month, s.BillingDay)
		if !s.Shared || s.Status == "paused" || month < m.Since || due.Before(s.StartedOn) || due.After(today) || (s.EndedOn != nil && due.After(*s.EndedOn)) {
			v.Add("month", "SERVICE_PAYMENT_MONTH_INVALID")
		}
		return m.Amount, v.OrNil()
	}
	return money.Amount{}, ErrNotFound
}
