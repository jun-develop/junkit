package recaptcha

import (
	"net/http"

	"go.uber.org/fx"
)

// Module provides a reCAPTCHA Verifier to the Fx graph.
var Module = fx.Module("recaptcha",
	fx.Provide(newVerifier),
)

// verifierParams are the Fx dependencies of newVerifier.
type verifierParams struct {
	fx.In

	Options    Options
	HTTPClient *http.Client `optional:"true"`
}

// newVerifier returns a MockVerifier when Options.Mock is set, otherwise a Client.
func newVerifier(p verifierParams) Verifier {
	if p.Options.Mock {
		return NewMockVerifier()
	}

	return NewClient(p.Options, p.HTTPClient)
}
