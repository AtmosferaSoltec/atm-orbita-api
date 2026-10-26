package domain_test

import (
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/loans/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/money"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"testing"
)

func amount(s string) money.Amount {
	a, err := money.ParseAmount(s)
	if err != nil {
		panic(err)
	}
	return a
}
func date(s string) dates.LocalDate {
	d, err := dates.Parse(s)
	if err != nil {
		panic(err)
	}
	return d
}
func movement(kind, value, on string) domain.Movement {
	return domain.Movement{ID: uuid.New(), Kind: kind, Amount: amount(value), Date: date(on), AppliedMonth: map[string]string{"payment": "2026-10"}[kind]}
}
func TestManualPlanAndFlexiblePayments(t *testing.T) {
	loan := movement("loan", "6000.00", "2026-10-01")
	require.NoError(t, loan.Validate(date("2026-10-24")))
	for _, value := range []string{"500", "1000"} {
		p := movement("payment", value, "2026-10-15")
		p.AppliedMonth = "2027-01"
		require.NoError(t, p.Validate(date("2026-10-24")))
		require.NoError(t, domain.ValidateLedger([]domain.Movement{loan, p}))
	}
	plan := []domain.Plan{{Month: "2026-10", Amount: amount("800")}, {Month: "2026-12", Amount: amount("500")}}
	require.NoError(t, domain.ValidatePlan(plan))
	require.NoError(t, domain.ValidatePlan(nil))
}
func TestLedgerRejectsOverpaymentAndBackdatedCorrections(t *testing.T) {
	loan := movement("loan", "3000", "2026-10-01")
	p := movement("payment", "3000", "2026-10-01")
	require.NoError(t, domain.ValidateLedger([]domain.Movement{p, loan}))
	require.ErrorIs(t, domain.ValidateLedger([]domain.Movement{p}), domain.ErrBalance)
	p.Date = date("2026-09-30")
	require.ErrorIs(t, domain.ValidateLedger([]domain.Movement{p, loan}), domain.ErrBalance)
	p.Date = date("2026-10-02")
	p.Amount = amount("3000.01")
	require.ErrorIs(t, domain.ValidateLedger([]domain.Movement{loan, p}), domain.ErrBalance)
	require.Error(t, domain.ValidateLedger([]domain.Movement{movement("loan", "999999999999.99", "2026-10-01"), movement("loan", "0.01", "2026-10-02")}))
}
func TestPlanValidation(t *testing.T) {
	for _, plan := range [][]domain.Plan{
		{{Month: "2026-13", Amount: amount("1")}}, {{Month: "1999-12", Amount: amount("1")}}, {{Month: "2026-10", Amount: amount("0")}}, {{Month: "2026-10", Amount: amount("1")}, {Month: "2026-10", Amount: amount("1")}},
	} {
		require.Error(t, domain.ValidatePlan(plan))
	}
}
func TestMovementValidation(t *testing.T) {
	today := date("2026-10-24")
	for _, m := range []domain.Movement{movement("other", "1", "2026-10-01"), movement("loan", "0", "2026-10-01"), movement("loan", "1", "2026-10-25"), movement("loan", "1", "1999-12-31")} {
		require.Error(t, m.Validate(today))
	}
	p := movement("payment", "1", "2026-10-01")
	p.AppliedMonth = ""
	require.Error(t, p.Validate(today))
	p.AppliedMonth = "2026-10"
	require.NoError(t, p.Validate(today))
}
