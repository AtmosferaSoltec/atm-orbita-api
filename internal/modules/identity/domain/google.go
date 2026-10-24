package domain

import "errors"

var (
	ErrGoogleUnavailable      = errors.New("google sign-in unavailable")
	ErrGoogleInvalid          = errors.New("invalid Google credential")
	ErrGoogleLinkRequired     = errors.New("existing account needs password confirmation")
	ErrGoogleIdentityConflict = errors.New("google identity already linked")
	ErrGoogleChallengeExpired = errors.New("google login challenge expired or consumed")
)
