package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

var (
	today      = dates.New(2026, time.October, 2)
	visa       = Card{ID: uuid.New(), Name: "Visa Clásica", Currency: "PEN"}
	mastercard = Card{ID: uuid.New(), Name: "Mastercard Oro", Currency: "PEN"}
	empty      = Card{ID: uuid.New(), Name: "Sin deuda", Currency: "PEN"}
)

func purchase(card Card, amount, currency string, due dates.LocalDate) Purchase {
	return Purchase{CardID: card.ID, Amount: decimal.RequireFromString(amount), Currency: currency, DueDate: due}
}

// Las compras pendientes de SampleData.kt.
var sample = []Purchase{
	purchase(visa, "240.00", "PEN", dates.New(2026, time.October, 5)),
	purchase(visa, "96.00", "PEN", dates.New(2026, time.October, 15)),
	purchase(mastercard, "189.90", "PEN", dates.New(2026, time.October, 15)),
	purchase(mastercard, "12.00", "USD", dates.New(2026, time.October, 15)),
}

func amounts(debt []CurrencyAmount) []string {
	var out []string
	for _, d := range debt {
		out = append(out, d.Currency+" "+d.Amount.StringFixed(2))
	}
	return out
}

func TestDebtOfTheSample(t *testing.T) {
	assert.Equal(t, []string{"PEN 525.90", "USD 12.00"}, amounts(Debt(sample, "PEN")))
	assert.Equal(t, []string{"USD 12.00", "PEN 525.90"}, amounts(Debt(sample, "USD")), "la principal primero")

	cards := CardsWithDebt([]Card{empty, mastercard, visa}, sample, "PEN")
	require.Len(t, cards, 3)
	assert.Equal(t, "Visa Clásica", cards[0].Name, "la fecha límite más próxima primero")
	assert.Equal(t, []string{"PEN 336.00"}, amounts(cards[0].Debt))
	assert.Equal(t, 2, cards[0].PendingCount)
	assert.Equal(t, "2026-10-05", cards[0].NextDueDate.String())
	assert.Equal(t, []string{"PEN 189.90", "USD 12.00"}, amounts(cards[1].Debt))
	assert.Equal(t, "Sin deuda", cards[2].Name, "las que no deben nada al final")
	assert.Nil(t, cards[2].NextDueDate)
	assert.Empty(t, cards[2].Debt)
}

func TestSummaryAndUrgency(t *testing.T) {
	s, ok := Summarize(sample, today)
	require.True(t, ok)
	assert.Equal(t, 4, s.PendingCount)
	assert.Equal(t, "2026-10-05", s.NextDueDate.String())
	assert.True(t, s.HasUrgent, "vence en 3 días")

	later := []Purchase{purchase(visa, "1.00", "PEN", dates.New(2026, time.October, 6))}
	s, _ = Summarize(later, today)
	assert.False(t, s.HasUrgent, "4 días no es urgente")
	overdue := []Purchase{purchase(visa, "1.00", "PEN", dates.New(2026, time.September, 30))}
	s, _ = Summarize(overdue, today)
	assert.True(t, s.HasUrgent, "vencida")

	_, ok = Summarize(nil, today)
	assert.False(t, ok)
}

func TestPurchaseValidation(t *testing.T) {
	var problems ValidationError
	ValidateAmount(nil, &problems)
	ValidateDescription(strings.Repeat("a", 501), &problems)
	yesterday := "2026-10-01"
	ValidateDueDate(&yesterday, today, &problems)
	ValidateDueDate(nil, today, &problems)
	ValidateCardName(" ", &problems)
	ValidateCurrency("XXX", &problems)
	var codes []string
	for _, f := range problems.Fields {
		codes = append(codes, f.Code)
	}
	assert.Equal(t, []string{CodeAmountRequired, CodeDescriptionTooLong, CodeDueDatePast, CodeInvalidFormat, CodeNameRequired, CodeCurrencyNotSupported}, codes)

	problems = ValidationError{}
	todayText := "2026-10-02"
	assert.Equal(t, today, ValidateDueDate(&todayText, today, &problems))
	assert.Empty(t, problems.Fields, "vencer hoy vale")
}
