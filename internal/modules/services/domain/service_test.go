package domain

import (
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
)

func amount(t *testing.T, text string) money.Amount {
	t.Helper()
	v, err := money.ParseAmount(text)
	require.NoError(t, err)
	return v
}

func subscription(t *testing.T) Service {
	return Service{Name: "Netflix", Color: "#1D4ED8", Currency: "PEN", Amount: amount(t, "30.00"), BillingDay: 31, StartedOn: dates.New(2026, time.January, 1), Status: "active", Shared: true, Members: []Member{{ID: uuid.New(), Name: "Ana", Amount: amount(t, "10.00"), Since: "2026-01"}}}
}

func TestManualSharesAndDatesAreValidated(t *testing.T) {
	s := subscription(t)
	require.NoError(t, s.Validate())
	s.Members[0].Amount = amount(t, "30.01")
	assert.Error(t, s.Validate())
	s.Members[0].Amount = amount(t, "10.00")
	s.Members[0].Since = "2025-12"
	assert.Error(t, s.Validate())
	s.Members[0].Since = "2026-13"
	assert.Error(t, s.Validate())
	s.Members[0].Since = "2026-01"
	s.Members = append(s.Members, s.Members[0])
	assert.Error(t, s.Validate())
}

func TestShortMonthsClampBillingDay(t *testing.T) {
	assert.Equal(t, "2026-02-28", DueDate("2026-02", 31).String())
	assert.Equal(t, "2028-02-29", DueDate("2028-02", 31).String())
	assert.Equal(t, "2026-04-30", DueDate("2026-04", 31).String())
}

func TestPaymentEligibilityAndServerAmount(t *testing.T) {
	s := subscription(t)
	today := dates.New(2026, time.March, 1)
	paid := dates.New(2026, time.February, 28)
	got, err := s.PaymentAmount(s.Members[0].ID, "2026-02", paid, today)
	require.NoError(t, err)
	assert.Equal(t, "10.00", got.String())
	_, err = s.PaymentAmount(s.Members[0].ID, "2026-03", paid, today)
	assert.Error(t, err)
	_, err = s.PaymentAmount(uuid.New(), "2026-02", paid, today)
	assert.ErrorIs(t, err, ErrNotFound)
	_, err = s.PaymentAmount(s.Members[0].ID, "2026-02", today.AddDays(1), today)
	assert.Error(t, err)
	s.Status = "paused"
	_, err = s.PaymentAmount(s.Members[0].ID, "2026-02", paid, today)
	assert.Error(t, err)
	s.Status = "cancelled"
	end := dates.New(2026, time.February, 15)
	s.EndedOn = &end
	_, err = s.PaymentAmount(s.Members[0].ID, "2026-02", paid, today)
	assert.Error(t, err)
}
