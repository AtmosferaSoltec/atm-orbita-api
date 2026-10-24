package security

import (
	"context"
	"crypto/subtle"
	"time"

	"google.golang.org/api/idtoken"

	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/app"
	"github.com/AtmosferaSoltec/atm-orbita-api/internal/modules/identity/domain"
)

// GoogleVerifier uses Google's maintained verifier (including cached signing
// keys). It never accepts an audience supplied by the caller.
type GoogleVerifier struct {
	ClientID  string
	validator *idtoken.Validator
}

func (v GoogleVerifier) Verify(ctx context.Context, credential, nonce string) (app.GoogleAccount, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if v.ClientID == "" {
		return app.GoogleAccount{}, domain.ErrGoogleUnavailable
	}
	validate := idtoken.Validate
	if v.validator != nil {
		validate = v.validator.Validate
	}
	payload, err := validate(ctx, credential, v.ClientID)
	if err != nil {
		return app.GoogleAccount{}, domain.ErrGoogleInvalid
	}
	return googleAccount(payload, nonce, time.Now())
}

func googleAccount(payload *idtoken.Payload, nonce string, now time.Time) (app.GoogleAccount, error) {
	if payload.Issuer != "https://accounts.google.com" && payload.Issuer != "accounts.google.com" {
		return app.GoogleAccount{}, domain.ErrGoogleInvalid
	}
	gotNonce, _ := payload.Claims["nonce"].(string)
	verified, _ := payload.Claims["email_verified"].(bool)
	email, _ := payload.Claims["email"].(string)
	name, _ := payload.Claims["name"].(string)
	if nonce == "" || subtle.ConstantTimeCompare([]byte(nonce), []byte(gotNonce)) != 1 ||
		!verified || payload.Subject == "" || email == "" || payload.Expires <= now.Unix() ||
		payload.IssuedAt > now.Add(time.Minute).Unix() {
		return app.GoogleAccount{}, domain.ErrGoogleInvalid
	}
	return app.GoogleAccount{Subject: payload.Subject, Email: email, Name: name}, nil
}
