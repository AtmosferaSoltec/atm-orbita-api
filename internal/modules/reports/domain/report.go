// Package domain son las reglas de los reportes: sumas por moneda sin mezclar,
// rankings y porcentajes. Go puro, sin Gin ni Postgres.
package domain

import (
	"fmt"
	"sort"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

// Tipos de movimiento.
const (
	KindIncome  = "income"
	KindExpense = "expense"
)

// CodeRangeInvalid: fecha mal formada, from posterior a to o un periodo de 10
// años o más.
const CodeRangeInvalid = "RANGE_INVALID"

// MaxRangeYears es el periodo más largo de un reporte.
const MaxRangeYears = 10

// FieldError es un campo con problema.
type FieldError struct {
	Field string
	Code  string
}

// ValidationError junta los problemas de una consulta.
type ValidationError struct {
	Fields []FieldError
}

func (e *ValidationError) Error() string { return fmt.Sprintf("validation failed: %v", e.Fields) }

// Category es lo que el reporte muestra de una categoría.
type Category struct {
	ID    uuid.UUID
	Name  string
	Kind  string
	Color string
}

// Row es la suma de los movimientos de una categoría en una moneda (la consulta).
type Row struct {
	Currency string
	Category Category
	Total    decimal.Decimal
}

// Line es una fila de un ranking, con su porcentaje ya calculado.
type Line struct {
	Category Category
	Total    decimal.Decimal
	Percent  decimal.Decimal
}

// CurrencyReport es el reporte de una moneda. Nunca mezcla monedas.
type CurrencyReport struct {
	Currency     string
	IncomeTotal  decimal.Decimal
	ExpenseTotal decimal.Decimal
	Balance      decimal.Decimal
	Incomes      []Line
	Expenses     []Line
}

// Period lee el periodo: las dos fechas, from no posterior a to y menos de 10
// años. to se recorta a hoy (no hay datos del futuro).
func Period(from, to string, today dates.LocalDate) (dates.LocalDate, dates.LocalDate, error) {
	start, errFrom := dates.Parse(from)
	end, errTo := dates.Parse(to)
	if errFrom != nil || errTo != nil || start.After(end) ||
		!end.Before(dates.FromTime(start.Time().AddDate(MaxRangeYears, 0, 0))) {
		return dates.LocalDate{}, dates.LocalDate{}, &ValidationError{Fields: []FieldError{{Field: "to", Code: CodeRangeInvalid}}}
	}
	if end.After(today) {
		end = today
	}
	return start, end, nil
}

// Percent es part × 100 ÷ whole con 1 decimal y HALF_UP; 0 si whole es cero
// (percentOf en Android).
func Percent(part, whole decimal.Decimal) decimal.Decimal {
	if whole.IsZero() {
		return decimal.Zero
	}
	return part.Mul(decimal.NewFromInt(100)).DivRound(whole, 1)
}

// Build arma un reporte por moneda: la principal primero, luego las demás por
// código. Cada ranking va de mayor a menor total; los empates, por nombre.
func Build(rows []Row, mainCurrency string) []CurrencyReport {
	byCurrency := map[string]*CurrencyReport{}
	var order []string
	for _, row := range rows {
		report, ok := byCurrency[row.Currency]
		if !ok {
			report = &CurrencyReport{Currency: row.Currency, Incomes: []Line{}, Expenses: []Line{}}
			byCurrency[row.Currency] = report
			order = append(order, row.Currency)
		}
		line := Line{Category: row.Category, Total: row.Total}
		if row.Category.Kind == KindIncome {
			report.Incomes = append(report.Incomes, line)
			report.IncomeTotal = report.IncomeTotal.Add(row.Total)
		} else {
			report.Expenses = append(report.Expenses, line)
			report.ExpenseTotal = report.ExpenseTotal.Add(row.Total)
		}
	}
	sort.Slice(order, func(i, j int) bool {
		if (order[i] == mainCurrency) != (order[j] == mainCurrency) {
			return order[i] == mainCurrency
		}
		return order[i] < order[j]
	})
	out := make([]CurrencyReport, 0, len(order))
	for _, currency := range order {
		report := byCurrency[currency]
		report.Balance = report.IncomeTotal.Sub(report.ExpenseTotal)
		rank(report.Incomes, report.IncomeTotal)
		rank(report.Expenses, report.ExpenseTotal)
		out = append(out, *report)
	}
	return out
}

func rank(lines []Line, total decimal.Decimal) {
	sort.SliceStable(lines, func(i, j int) bool {
		if c := lines[i].Total.Cmp(lines[j].Total); c != 0 {
			return c > 0
		}
		return lines[i].Category.Name < lines[j].Category.Name
	})
	for i := range lines {
		lines[i].Percent = Percent(lines[i].Total, total)
	}
}
