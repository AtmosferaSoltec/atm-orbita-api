package money

import (
	"encoding/json"
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Vectores de atm-orbita-android: docs/06 y MoneyRulesTest.kt.

func TestDecimalsAddUpExactly(t *testing.T) {
	sum := decimal.RequireFromString("0.1").Add(decimal.RequireFromString("0.2"))
	assert.True(t, sum.Equal(decimal.RequireFromString("0.3")))
}

func TestParseAmountAcceptsTheApiFormat(t *testing.T) {
	for text, want := range map[string]string{
		"45.52":           "45.52",
		"45.5":            "45.50",
		"45":              "45.00",
		"0":               "0.00",
		"999999999999.99": "999999999999.99",
	} {
		amount, err := ParseAmount(text)
		require.NoError(t, err, text)
		assert.Equal(t, want, amount.String(), text)
	}
}

func TestParseAmountRejectsEverythingElse(t *testing.T) {
	for _, text := range []string{"45.523", "-5", "45,52", "1e3", "", " 45.52", "+5", ".5", "5.", "1234567890123"} {
		_, err := ParseAmount(text)
		assert.ErrorIs(t, err, ErrInvalidAmount, text)
	}
}

func TestZeroIsParsedButIsNotPositive(t *testing.T) {
	amount, err := ParseAmount("0")
	require.NoError(t, err)
	assert.False(t, amount.IsPositive())
}

func TestAmountTravelsAsText(t *testing.T) {
	out, err := json.Marshal(struct {
		Balance Amount `json:"balance"`
	}{NewAmount(decimal.RequireFromString("1245.8"))})
	require.NoError(t, err)
	assert.JSONEq(t, `{"balance":"1245.80"}`, string(out))

	var in struct {
		Amount Amount `json:"amount"`
	}
	require.NoError(t, json.Unmarshal([]byte(`{"amount":"45.52"}`), &in))
	assert.Equal(t, "45.52", in.Amount.String())
}

func TestAJsonNumberIsNotAnAmount(t *testing.T) {
	var in struct {
		Amount Amount `json:"amount"`
	}
	err := json.Unmarshal([]byte(`{"amount":45.52}`), &in)
	assert.ErrorIs(t, err, ErrInvalidAmount)
}

func TestRoundingIsHalfUp(t *testing.T) {
	// 7,166.30 ÷ 3.20 = 2,239.46875 → 2,239.47
	savings := decimal.RequireFromString("7166.30").Div(decimal.RequireFromString("3.20"))
	assert.Equal(t, "2239.47", NewAmount(savings).String())
	assert.Equal(t, "0.13", NewAmount(decimal.RequireFromString("0.125")).String())
}

func TestRate(t *testing.T) {
	rate, err := ParseRate("3.2")
	require.NoError(t, err)
	assert.Equal(t, "3.200000", rate.String())

	_, err = ParseRate("3.1234567")
	assert.ErrorIs(t, err, ErrInvalidRate)

	// Cambio que deja una transferencia: entra 63.40 por 20.00 que salen.
	implied := NewRate(decimal.RequireFromString("63.40").Div(decimal.RequireFromString("20.00")))
	assert.Equal(t, "3.170000", implied.String())
}
