package domain

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func TestTransferRateRules(t *testing.T) {
	d := decimal.RequireFromString
	cases := []struct {
		name           string
		fromCur, toCur string
		from, to       string
		rate           *string
		want           []string
		wantRate       string
	}{
		{"misma moneda y montos iguales", "PEN", "PEN", "500.00", "500.00", nil, nil, ""},
		{"misma moneda con montos distintos", "PEN", "PEN", "500.00", "499.00", nil, []string{CodeAmountsMustMatch}, ""},
		{"misma moneda con tipo de cambio", "PEN", "PEN", "500.00", "500.00", str("1"), []string{CodeAmountsMustMatch}, ""},
		{"monedas distintas sin tipo de cambio", "USD", "PEN", "20.00", "64.00", nil, []string{CodeRateRequired}, ""},
		{"monedas distintas con cero", "USD", "PEN", "20.00", "64.00", str("0"), []string{CodeRateRequired}, ""},
		{"monedas distintas con texto", "USD", "PEN", "20.00", "64.00", str("tres"), []string{CodeRateRequired}, ""},
		{"monedas distintas: se guarda tal cual", "USD", "PEN", "20.00", "63.40", str("3.17"), nil, "3.17"},
	}
	for _, c := range cases {
		var problems ValidationError
		rate := CheckTransferRate(c.fromCur, c.toCur, d(c.from), d(c.to), true, c.rate, &problems)
		assert.Equal(t, c.want, codes(problems), c.name)
		if c.wantRate == "" {
			assert.Nil(t, rate, c.name)
		} else {
			assert.True(t, rate.Equal(d(c.wantRate)), c.name)
		}
	}
}

func TestSameTransferComparesEverythingButAMissingDate(t *testing.T) {
	rate := decimal.RequireFromString("3.2")
	a := Transfer{FromAmount: decimal.RequireFromString("20"), ToAmount: decimal.RequireFromString("64"), ExchangeRate: &rate, Note: "Cambio"}
	b := a
	b.OccurredOn = a.OccurredOn.AddDays(1)
	assert.True(t, SameTransfer(a, b, false))
	assert.False(t, SameTransfer(a, b, true))
	b.ExchangeRate = nil
	assert.False(t, SameTransfer(a, b, false))
}
