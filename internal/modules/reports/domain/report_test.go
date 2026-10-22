package domain

import (
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

func row(currency, kind, name, total string) Row {
	return Row{Currency: currency, Category: Category{Name: name, Kind: kind}, Total: decimal.RequireFromString(total)}
}

// Las sumas de septiembre de SampleData.kt, en el desorden en que las da la base.
var september = []Row{
	row("PEN", KindExpense, "Salud", "148.00"),
	row("PEN", KindIncome, "Otros ingresos", "100.00"),
	row("PEN", KindExpense, "Alimentación", "612.40"),
	row("PEN", KindExpense, "Otros", "183.00"),
	row("PEN", KindIncome, "Sueldo", "3500.00"),
	row("PEN", KindExpense, "Ocio", "190.00"),
	row("PEN", KindExpense, "Vivienda", "800.00"),
	row("PEN", KindIncome, "Freelance", "600.00"),
	row("PEN", KindExpense, "Transporte", "215.20"),
}

func names(lines []Line) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Category.Name)
	}
	return out
}

func percents(lines []Line) []string {
	var out []string
	for _, l := range lines {
		out = append(out, l.Percent.StringFixed(1))
	}
	return out
}

func TestSeptemberOfTheSample(t *testing.T) {
	reports := Build(september, "PEN")
	require.Len(t, reports, 1)
	r := reports[0]
	assert.Equal(t, "4200.00", r.IncomeTotal.StringFixed(2))
	assert.Equal(t, "2148.60", r.ExpenseTotal.StringFixed(2))
	assert.Equal(t, "2051.40", r.Balance.StringFixed(2))
	assert.Equal(t, []string{"Vivienda", "Alimentación", "Transporte", "Ocio", "Otros", "Salud"}, names(r.Expenses))
	assert.Equal(t, []string{"37.2", "28.5", "10.0", "8.8", "8.5", "6.9"}, percents(r.Expenses))
	assert.Equal(t, []string{"Sueldo", "Freelance", "Otros ingresos"}, names(r.Incomes))
	assert.Equal(t, []string{"83.3", "14.3", "2.4"}, percents(r.Incomes))
}

func TestPercentRoundsHalfUpAndZeroIsZero(t *testing.T) {
	d := decimal.RequireFromString
	assert.Equal(t, "0.0", Percent(d("10"), decimal.Zero).StringFixed(1))
	assert.Equal(t, "33.3", Percent(d("1"), d("3")).StringFixed(1))
	assert.Equal(t, "12.5", Percent(d("1"), d("8")).StringFixed(1))
	assert.Equal(t, "0.1", Percent(d("1"), d("2000")).StringFixed(1), "0.05 sube a 0.1")
}

func TestCurrenciesNeverMixAndTheMainOneGoesFirst(t *testing.T) {
	reports := Build([]Row{
		row("USD", KindExpense, "Ocio", "12.00"),
		row("EUR", KindIncome, "Sueldo", "5.00"),
		row("PEN", KindIncome, "Sueldo", "100.00"),
	}, "PEN")
	var currencies []string
	for _, r := range reports {
		currencies = append(currencies, r.Currency)
	}
	assert.Equal(t, []string{"PEN", "EUR", "USD"}, currencies)
	assert.Empty(t, reports[0].Expenses)
	assert.NotNil(t, reports[0].Expenses, "una moneda solo con ingresos trae expenses: []")
	assert.Equal(t, "-12.00", reports[2].Balance.StringFixed(2))
	assert.Empty(t, Build(nil, "PEN"))
}

func TestTiesGoByName(t *testing.T) {
	r := Build([]Row{row("PEN", KindExpense, "Salud", "10"), row("PEN", KindExpense, "Ocio", "10")}, "PEN")[0]
	assert.Equal(t, []string{"Ocio", "Salud"}, names(r.Expenses))
}

func TestPeriodIsClippedToToday(t *testing.T) {
	today := dates.New(2026, time.October, 2)
	from, to, err := Period("2026-10-01", "2026-10-31", today)
	require.NoError(t, err)
	assert.Equal(t, "2026-10-01", from.String())
	assert.Equal(t, "2026-10-02", to.String())

	for _, bad := range [][2]string{{"2026-10-31", "2026-10-01"}, {"", "2026-10-01"}, {"2016-10-02", "2026-10-02"}, {"2026-10-01", "mañana"}} {
		_, _, err := Period(bad[0], bad[1], today)
		var problems *ValidationError
		require.ErrorAs(t, err, &problems, bad)
		assert.Equal(t, CodeRangeInvalid, problems.Fields[0].Code)
	}
}
