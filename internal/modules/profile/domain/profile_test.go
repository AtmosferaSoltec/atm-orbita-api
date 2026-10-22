package domain

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Vectores de atm-orbita-android: MoneyRulesTest.kt y ORBITA_SPEC §9.

func d(text string) decimal.Decimal { return decimal.RequireFromString(text) }

func pair(rate string) FxPair {
	r := d(rate)
	return FxPair{Main: "PEN", Secondary: "USD", Rate: &r}
}

func TestDollarsToSolesWithTheManualRate(t *testing.T) {
	got, ok := pair("3.20").Convert(d("20.00"), "USD", "PEN")
	require.True(t, ok)
	assert.Equal(t, "64.00", got.StringFixed(2))
}

func TestSolesBackToDollarsWithTheInverse(t *testing.T) {
	got, ok := pair("3.20").Convert(d("64.00"), "PEN", "USD")
	require.True(t, ok)
	assert.Equal(t, "20.00", got.StringFixed(2))

	// 7,166.30 ÷ 3.20 = 2,239.46875 → 2,239.47 (HALF_UP, una sola vez).
	got, _ = pair("3.20").Convert(d("7166.30"), "PEN", "USD")
	assert.Equal(t, "2239.47", got.StringFixed(2))
}

func TestACurrencyOutsideThePairHasNoRate(t *testing.T) {
	_, ok := pair("3.20").Convert(d("10.00"), "EUR", "PEN")
	assert.False(t, ok)
}

func TestWithoutARateNothingIsConverted(t *testing.T) {
	unset := FxPair{Main: "PEN", Secondary: "USD"}
	_, ok := unset.Convert(d("20.00"), "USD", "PEN")
	assert.False(t, ok)
	same, ok := unset.Convert(d("20.00"), "PEN", "PEN")
	assert.True(t, ok)
	assert.Equal(t, "20.00", same.StringFixed(2))
}

func TestRateBetweenBothWays(t *testing.T) {
	r, _ := pair("3.20").RateBetween("USD", "PEN")
	assert.Equal(t, "3.200000", r.StringFixed(6))
	r, _ = pair("3.20").RateBetween("PEN", "USD")
	assert.Equal(t, "0.312500", r.StringFixed(6))
	assert.Equal(t, "0.312500", Inverse(d("3.20")).StringFixed(6))
}

func TestTheSecondaryCurrencyAProfileStartsWith(t *testing.T) {
	assert.Equal(t, "USD", SecondaryFor("PEN"))
	assert.Equal(t, "USD", SecondaryFor("MXN"))
	assert.Equal(t, "PEN", SecondaryFor("USD"))
}

func TestWithoutARateOnlyTheMainCurrencyIsUsable(t *testing.T) {
	p := Profile{FxPair: FxPair{Main: "PEN", Secondary: "USD"}}
	assert.NoError(t, p.UsableCurrency("PEN"))
	assert.ErrorIs(t, p.UsableCurrency("USD"), ErrFxNotConfigured)

	p.FxPair = pair("3.20")
	assert.NoError(t, p.UsableCurrency("USD"))
}

func TestDisplayFollowsThePair(t *testing.T) {
	assert.Equal(t, "USD", DisplayAfterPairChange("USD", "USD", "PEN"))
	assert.Equal(t, "MXN", DisplayAfterPairChange("USD", "MXN", "EUR"))
}
