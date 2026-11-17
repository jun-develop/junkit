package mailer

import (
	"net/http"

	"go.uber.org/fx"
	"go.uber.org/zap"
)

// Module provides a Sender to the Fx graph.
var Module = fx.Module("mailer",
	fx.Provide(newSender),
)

// params are newSender's Fx dependencies; the HTTP client is optional so SMTP needs no
// httpclient.Module.
type params struct {
	fx.In

	Opts       Options
	HTTPClient *http.Client `optional:"true"`
	Logger     *zap.Logger
}

// newSender adapts New to Fx.
func newSender(p params) (Sender, error) {
	return New(p.Opts, p.HTTPClient, p.Logger)
}
