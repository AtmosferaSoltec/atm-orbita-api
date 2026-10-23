package mail

import (
	"bytes"
	"embed"
	"fmt"
	htmltemplate "html/template"
	texttemplate "text/template"
	"time"
)

// Los .html son la compilación de los .mjml (ver templates/README.md).
//
//go:embed templates/*.html
var templateFiles embed.FS

var htmlTemplates = htmltemplate.Must(htmltemplate.ParseFS(templateFiles, "templates/*.html"))

var textTemplates = texttemplate.Must(texttemplate.New("reset_code").Parse(
	`Recupera tu contraseña de Orbita

Escribe este código en la app para crear una contraseña nueva:

    {{.Code}}

Vence en {{.Minutes}} minutos y solo sirve una vez.

Si no lo pediste, ignora este correo: tu contraseña no cambia.
`))

func init() {
	texttemplate.Must(textTemplates.New("password_changed").Parse(
		`Tu contraseña de Orbita se cambió

La contraseña de tu cuenta de Orbita se acaba de cambiar y se cerraron las sesiones abiertas en tus dispositivos.

Si no fuiste tú, recupera tu contraseña desde la app ahora mismo.
`))
}

type resetCodeData struct {
	Code    string
	Minutes int
}

// ResetCodeMessage es el correo con el código para recuperar la contraseña.
func ResetCodeMessage(to, code string, validFor time.Duration) (Message, error) {
	data := resetCodeData{Code: code, Minutes: int(validFor.Minutes())}
	return render(to, fmt.Sprintf("Tu código de Orbita: %s", code), "reset_code", data)
}

// PasswordChangedMessage avisa que la contraseña se cambió.
func PasswordChangedMessage(to string) (Message, error) {
	return render(to, "Tu contraseña de Orbita se cambió", "password_changed", nil)
}

func render(to, subject, name string, data any) (Message, error) {
	var html, text bytes.Buffer
	if err := htmlTemplates.ExecuteTemplate(&html, name+".html", data); err != nil {
		return Message{}, fmt.Errorf("render %s html: %w", name, err)
	}
	if err := textTemplates.ExecuteTemplate(&text, name, data); err != nil {
		return Message{}, fmt.Errorf("render %s text: %w", name, err)
	}
	return Message{To: to, Subject: subject, HTML: html.String(), Text: text.String()}, nil
}
