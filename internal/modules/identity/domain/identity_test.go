package domain

import (
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestEmailIsTrimmedAndLowercased(t *testing.T) {
	assert.Equal(t, "ana@correo.com", NormalizeEmail("  Ana@Correo.COM "))
}

func TestEmailFormat(t *testing.T) {
	for email, valid := range map[string]bool{
		"ana@correo.com":                   true,
		"ana.torres@x.pe":                  true,
		"ana@correo":                       false,
		"ana correo@x.com":                 false,
		"@correo.com":                      false,
		"":                                 false,
		strings.Repeat("a", 250) + "@x.pe": false,
	} {
		var problems ValidationError
		ValidateEmail(email, &problems)
		assert.Equal(t, valid, problems.OrNil() == nil, email)
	}
}

func TestPasswordLength(t *testing.T) {
	cases := map[string]string{
		"1234567":                CodePasswordTooShort,
		"12345678":               "",
		"ñandú123":               "", // 8 caracteres aunque sean más bytes
		strings.Repeat("a", 128): "",
		strings.Repeat("a", 129): CodePasswordTooLong,
	}
	for password, code := range cases {
		var problems ValidationError
		ValidateNewPassword(password, &problems)
		if code == "" {
			assert.NoError(t, problems.OrNil(), password)
		} else {
			assert.Equal(t, code, problems.Fields[0].Code, password)
		}
	}
}
