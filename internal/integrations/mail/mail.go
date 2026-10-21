// Package mail envía correos detrás de un puerto, para poder cambiar de
// proveedor sin tocar los módulos. Resend llega con el plan 11.
package mail

import (
	"context"
	"log/slog"
)

// Message es un correo a una sola persona, en HTML y en texto plano.
type Message struct {
	To      string
	Subject string
	HTML    string
	Text    string
}

// Mailer envía un correo.
type Mailer interface {
	Send(ctx context.Context, message Message) error
}

// Console escribe el correo en el log en lugar de enviarlo. Solo para
// desarrollo: el código de recuperación de contraseña se lee ahí.
type Console struct {
	Log *slog.Logger
}

// Send registra el correo.
func (c Console) Send(_ context.Context, message Message) error {
	c.Log.Info("email (not sent, development)",
		"to", message.To, "subject", message.Subject, "text", message.Text)
	return nil
}
