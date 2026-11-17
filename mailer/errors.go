package mailer

import "errors"

var (
	// ErrInvalidMessage is returned (wrapped) when a Message fails validation before sending.
	ErrInvalidMessage = errors.New("mailer: invalid message")

	// ErrTLSRequired is returned (wrapped) when the SMTP server offers no STARTTLS and
	// SMTPOptions.AllowPlaintext is not set.
	ErrTLSRequired = errors.New("mailer: smtp server does not offer STARTTLS")
)
