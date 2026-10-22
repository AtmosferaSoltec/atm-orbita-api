package domain

import (
	"testing"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
)

func amount(text string) decimal.Decimal { return decimal.RequireFromString(text) }

// Las cuentas de SampleData.kt en Android, con el saldo con que terminan.
var sampleAccounts = []Account{
	{Name: "Cuenta Dólares", Type: AccountSavings, Currency: "USD", Balance: amount("250.00"), IncludeInSavings: true},
	{Name: "Efectivo", Type: AccountCash, Currency: "PEN", Balance: amount("320.50"), IncludeInSavings: true},
	{Name: "Débito principal", Type: AccountDebit, Currency: "PEN", Balance: amount("1245.80"), IncludeInSavings: true},
	{Name: "Ahorros", Type: AccountSavings, Currency: "PEN", Balance: amount("4800.00"), IncludeInSavings: true},
	{Name: "Billetera digital", Type: AccountOther, Currency: "PEN", Balance: amount("85.00"), IncludeInSavings: false},
}

func rate(text string) *decimal.Decimal {
	r := amount(text)
	return &r
}

func totals(s Savings) map[string]string {
	out := map[string]string{}
	for _, t := range s.Totals {
		out[t.Currency] = t.Amount.StringFixed(2)
	}
	return out
}

func TestSavingsOfTheSampleInSolesAndDollars(t *testing.T) {
	s := ComputeSavings(sampleAccounts, Fx{Main: "PEN", Secondary: "USD", Rate: rate("3.20")})
	// 7,166.30 / 3.20 = 2,239.46875: se redondea una sola vez, al final.
	assert.Equal(t, map[string]string{"PEN": "7166.30", "USD": "2239.47"}, totals(s))
	assert.Equal(t, "PEN", s.Totals[0].Currency)
	assert.Equal(t, 4, s.IncludedCount)
	assert.Equal(t, 5, s.ActiveCount)
}

func TestWithoutARateOnlyTheMainCurrencyAddsUp(t *testing.T) {
	s := ComputeSavings(sampleAccounts, Fx{Main: "PEN", Secondary: "USD"})
	assert.Equal(t, map[string]string{"PEN": "6366.30"}, totals(s))
}

func TestAnAccountOutOfSavingsOrOutOfThePairDoesNotAddUp(t *testing.T) {
	wallet := sampleAccounts[4:]
	assert.Equal(t, map[string]string{"PEN": "0.00", "USD": "0.00"},
		totals(ComputeSavings(wallet, Fx{Main: "PEN", Secondary: "USD", Rate: rate("3.20")})))

	euros := []Account{{Currency: "EUR", Balance: amount("100.00"), IncludeInSavings: true}}
	assert.Equal(t, map[string]string{"PEN": "0.00", "USD": "0.00"},
		totals(ComputeSavings(euros, Fx{Main: "PEN", Secondary: "USD", Rate: rate("3.20")})))
}

func TestRequireUsable(t *testing.T) {
	without := Fx{Main: "PEN", Secondary: "USD"}
	assert.NoError(t, without.RequireUsable("PEN"))
	assert.ErrorIs(t, without.RequireUsable("USD"), ErrFxNotConfigured)
	with := Fx{Main: "PEN", Secondary: "USD", Rate: rate("3.20")}
	assert.NoError(t, with.RequireUsable("USD"))
}

func TestAccountValidation(t *testing.T) {
	var problems ValidationError
	ValidateAccountType("wallet", &problems)
	ValidateCurrency("XXX", &problems)
	negative, bad, number := "-5.00", "12.345", "100"
	ValidateInitialBalance(&negative, &problems)
	ValidateInitialBalance(&bad, &problems)
	assert.Equal(t, []string{CodeTypeInvalid, CodeCurrencyNotSupported, CodeAmountInvalid, CodeAmountInvalid}, codes(problems))

	problems = ValidationError{}
	assert.True(t, ValidateInitialBalance(nil, &problems).IsZero())
	assert.Equal(t, "100", ValidateInitialBalance(&number, &problems).String())
	for _, ok := range []AccountType{AccountCash, AccountDebit, AccountSavings, AccountOther} {
		ValidateAccountType(ok, &problems)
	}
	ValidateCurrency("MXN", &problems)
	assert.Empty(t, problems.Fields)
}
