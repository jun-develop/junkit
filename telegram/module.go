package telegram

import (
	"net/http"

	"go.uber.org/fx"
)

// Module provides Client to the Fx graph.
var Module = fx.Module("telegram",
	fx.Provide(newModuleClient),
)

// moduleParams are Module's dependencies; the *http.Client is optional.
type moduleParams struct {
	fx.In

	Options    Options
	HTTPClient *http.Client `optional:"true"`
}

// newModuleClient builds the Client Module provides.
func newModuleClient(p moduleParams) Client {
	return NewClient(p.Options, p.HTTPClient)
}
