package domain

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/platform/dates"
)

func str(s string) *string { return &s }

func TestValidateAmount(t *testing.T) {
	for _, bad := range []*string{nil, str("0"), str("0.00"), str("-5.00"), str("12.345"), str("abc"), str("")} {
		var problems ValidationError
		ValidateAmount(bad, &problems)
		assert.Equal(t, []string{CodeAmountRequired}, codes(problems))
	}
	var problems ValidationError
	assert.Equal(t, "45.52", ValidateAmount(str("45.52"), &problems).StringFixed(2))
	assert.Empty(t, problems.Fields)
}

func TestValidateDescriptionCountsCharacters(t *testing.T) {
	var problems ValidationError
	assert.Equal(t, "Almuerzo", ValidateDescription("  Almuerzo ", &problems))
	ValidateDescription(strings.Repeat("ñ", 500), &problems)
	assert.Empty(t, problems.Fields)
	ValidateDescription(strings.Repeat("ñ", 501), &problems)
	assert.Equal(t, []string{CodeDescriptionTooLong}, codes(problems))
}

func TestValidateDateIsNeverAfterToday(t *testing.T) {
	today := dates.New(2026, time.October, 2)
	var problems ValidationError
	assert.Equal(t, today, ValidateDate(nil, today, &problems))
	assert.Equal(t, dates.New(2020, time.January, 1), ValidateDate(str("2020-01-01"), today, &problems))
	assert.Empty(t, problems.Fields)

	ValidateDate(str("2026-10-03"), today, &problems)
	ValidateDate(str("2026-02-30"), today, &problems)
	assert.Equal(t, []string{CodeFutureDate, CodeInvalidFormat}, codes(problems))
}

func TestValidateRange(t *testing.T) {
	var problems ValidationError
	from, to := ValidateRange("2026-09-01", "2026-09-30", &problems)
	assert.Empty(t, problems.Fields)
	assert.Equal(t, "2026-09-01", from.String())
	assert.Equal(t, "2026-09-30", to.String())
	ValidateRange("2016-10-03", "2026-10-02", &problems)
	assert.Empty(t, problems.Fields, "casi 10 años vale")

	for _, r := range [][2]string{{"2026-10-01", "2026-09-30"}, {"2016-10-02", "2026-10-02"}} {
		problems = ValidationError{}
		ValidateRange(r[0], r[1], &problems)
		assert.Equal(t, []string{CodeRangeInvalid}, codes(problems), r)
	}
	problems = ValidationError{}
	ValidateRange("", "2026-13-01", &problems)
	assert.Equal(t, []string{CodeInvalidFormat, CodeInvalidFormat}, codes(problems))
}

func TestSearchPatternEscapesWildcards(t *testing.T) {
	assert.Nil(t, SearchPattern("  "))
	assert.Equal(t, "%super%", *SearchPattern(" super "))
	assert.Equal(t, `%50\% de\_scuento \\%`, *SearchPattern(`50% de_scuento \`))
}
