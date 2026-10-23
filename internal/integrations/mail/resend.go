package mail

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"time"
)

// resendEndpoint es la API HTTP de Resend para enviar un correo.
const resendEndpoint = "https://api.resend.com/emails"

// Resend envía correos con la API HTTP de Resend. From es MAIL_FROM, de un
// dominio verificado en Resend.
type Resend struct {
	APIKey string
	From   string
	// Client es opcional; vacío usa uno con 10 s de tiempo máximo.
	Client *http.Client
	// Endpoint es opcional, para pruebas.
	Endpoint string
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	HTML    string   `json:"html"`
	Text    string   `json:"text"`
}

// Send envía el correo. Un error se puede reintentar (Queue lo hace).
func (r Resend) Send(ctx context.Context, message Message) error {
	body, err := json.Marshal(resendRequest{
		From: r.From, To: []string{message.To}, Subject: message.Subject, HTML: message.HTML, Text: message.Text,
	})
	if err != nil {
		return err
	}
	endpoint := r.Endpoint
	if endpoint == "" {
		endpoint = resendEndpoint
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return err
	}
	request.Header.Set("Authorization", "Bearer "+r.APIKey)
	request.Header.Set("Content-Type", "application/json")
	client := r.Client
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	response, err := client.Do(request)
	if err != nil {
		return fmt.Errorf("resend: %w", err)
	}
	defer func() { _ = response.Body.Close() }()
	if response.StatusCode/100 != 2 {
		// La respuesta de error de Resend no trae el contenido del correo.
		detail, _ := io.ReadAll(io.LimitReader(response.Body, 512))
		return fmt.Errorf("resend: status %d: %s", response.StatusCode, detail)
	}
	return nil
}
