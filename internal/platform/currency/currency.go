// Package currency es el catálogo de monedas que la app permite elegir (el de
// core/Formatters.kt en Android). Es un dato, no una regla: lo usan el registro
// y el perfil.
package currency

// Currency es una moneda admitida: código ISO 4217, símbolo y nombre en español.
type Currency struct {
	Code   string `json:"code"`
	Symbol string `json:"symbol"`
	Name   string `json:"name"`
}

// Default es la moneda principal cuando la app no envía una.
const Default = "PEN"

// Supported, en el orden en que la app las muestra.
var Supported = []Currency{
	{Code: "PEN", Symbol: "S/", Name: "Soles"},
	{Code: "USD", Symbol: "US$", Name: "Dólares"},
	{Code: "EUR", Symbol: "€", Name: "Euros"},
	{Code: "MXN", Symbol: "MX$", Name: "Pesos mexicanos"},
	{Code: "COP", Symbol: "COL$", Name: "Pesos colombianos"},
	{Code: "CLP", Symbol: "CLP$", Name: "Pesos chilenos"},
	{Code: "ARS", Symbol: "AR$", Name: "Pesos argentinos"},
	{Code: "BOB", Symbol: "Bs", Name: "Bolivianos"},
	{Code: "BRL", Symbol: "R$", Name: "Reales"},
}

// IsSupported dice si code está en el catálogo.
func IsSupported(code string) bool {
	for _, c := range Supported {
		if c.Code == code {
			return true
		}
	}
	return false
}
