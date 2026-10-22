package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func codes(problems ValidationError) []string {
	var out []string
	for _, f := range problems.Fields {
		out = append(out, f.Code)
	}
	return out
}

func TestValidateNameCleansAndCountsCharacters(t *testing.T) {
	var problems ValidationError
	assert.Equal(t, "Comida rápida", ValidateName("  Comida   rápida ", &problems))
	assert.Empty(t, problems.Fields)

	ValidateName("   ", &problems)
	assert.Equal(t, []string{CodeNameRequired}, codes(problems))

	// 60 letras con tilde son 120 bytes, pero 60 caracteres: vale.
	problems = ValidationError{}
	ValidateName(strings.Repeat("á", 60), &problems)
	assert.Empty(t, problems.Fields)
	ValidateName(strings.Repeat("á", 61), &problems)
	assert.Equal(t, []string{CodeNameTooLong}, codes(problems))
}

func TestValidateColorAndKind(t *testing.T) {
	var problems ValidationError
	for _, ok := range []string{"#0E7490", "#abcdef"} {
		ValidateColor(ok, &problems)
	}
	ValidateKind(KindIncome, &problems)
	ValidateKind(KindExpense, &problems)
	assert.Empty(t, problems.Fields)

	for _, bad := range []string{"", "0E7490", "#0E749", "#0E74900", "#GGGGGG"} {
		problems = ValidationError{}
		ValidateColor(bad, &problems)
		assert.Equal(t, []string{CodeColorInvalid}, codes(problems), bad)
	}
	problems = ValidationError{}
	ValidateKind("transfer", &problems)
	assert.Equal(t, []string{CodeKindInvalid}, codes(problems))
}

func TestNameKeyIgnoresCaseAndAccentsButKeepsEnye(t *testing.T) {
	key := func(name string) string { return Category{Name: name}.NameKey() }
	assert.Equal(t, key("Alimentación"), key("ALIMENTACION"))
	assert.Equal(t, key("Ocio"), key("ocio"))
	assert.NotEqual(t, key("Año"), key("Ano"))
}

func TestInitialCategoriesHaveValidNamesAndColors(t *testing.T) {
	assert.Len(t, InitialCategories, 9)
	seen := map[string]bool{}
	for _, c := range InitialCategories {
		var problems ValidationError
		ValidateName(c.Name, &problems)
		ValidateColor(c.Color, &problems)
		ValidateKind(c.Kind, &problems)
		assert.Empty(t, problems.Fields, c.Name)
		k := string(c.Kind) + "/" + Category{Name: c.Name}.NameKey()
		assert.False(t, seen[k], "duplicada: %s", c.Name)
		seen[k] = true
	}
}
