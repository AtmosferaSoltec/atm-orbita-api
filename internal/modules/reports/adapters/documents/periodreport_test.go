package documents_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	adapter "github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/adapters/documents"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/reports/domain"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

func line(name string, total string) domain.Line {
	return domain.Line{Category: domain.Category{ID: uuid.New(), Name: name}, Total: decimal.RequireFromString(total)}
}

func summary(t *testing.T, currencies ...domain.CurrencyReport) app.Summary {
	t.Helper()
	from, err := dates.Parse("2026-10-01")
	require.NoError(t, err)
	to, err := dates.Parse("2026-10-31")
	require.NoError(t, err)
	return app.Summary{From: from, To: to, Currencies: currencies}
}

func TestRequestFormatsMoneyWithTwoDecimalsAndPercentWithOne(t *testing.T) {
	food := line("Comida", "700")
	food.Percent = decimal.RequireFromString("35.8")
	req := adapter.Request(summary(t, domain.CurrencyReport{
		Currency: "PEN", IncomeTotal: decimal.RequireFromString("3200"), ExpenseTotal: decimal.RequireFromString("3215.5"),
		Balance: decimal.RequireFromString("-15.5"), Expenses: []domain.Line{food}, Incomes: []domain.Line{},
	}))
	assert.Equal(t, "2026-10-01", req.From)
	assert.Equal(t, "2026-10-31", req.To)
	got := req.Currencies[0]
	assert.Equal(t, "3200.00", got.IncomeTotal)
	assert.Equal(t, "3215.50", got.ExpenseTotal)
	assert.Equal(t, "-15.50", got.Balance)
	assert.Equal(t, "Comida", got.Expenses[0].Name)
	assert.Equal(t, "700.00", got.Expenses[0].Total)
	assert.Equal(t, "35.8", got.Expenses[0].Percent)
	assert.NotNil(t, got.Incomes, "an empty ranking is [] and not null")
}

func TestRankingsOverFiftyRowsFoldTheRestIntoOthers(t *testing.T) {
	var lines []domain.Line
	for i := range 60 {
		lines = append(lines, line(fmt.Sprintf("Cat %02d", i), "10"))
	}
	req := adapter.Request(summary(t, domain.CurrencyReport{
		Currency: "PEN", ExpenseTotal: decimal.NewFromInt(600), Expenses: lines,
	}))
	rows := req.Currencies[0].Expenses
	require.Len(t, rows, 50)
	assert.Equal(t, "Otros", rows[49].Name)
	assert.Equal(t, "110.00", rows[49].Total, "11 rows of 10")
	assert.Equal(t, "18.3", rows[49].Percent)
}

func TestLongNamesAreClippedAndCurrenciesCapped(t *testing.T) {
	long := line(strings.Repeat("ñ", 100), "1")
	var currencies []domain.CurrencyReport
	for i := range 15 {
		currencies = append(currencies, domain.CurrencyReport{Currency: fmt.Sprintf("C%02d", i), Expenses: []domain.Line{long}})
	}
	req := adapter.Request(summary(t, currencies...))
	require.Len(t, req.Currencies, 12)
	name := []rune(req.Currencies[0].Expenses[0].Name)
	assert.Len(t, name, 80)
	assert.Equal(t, '…', name[79])
}
