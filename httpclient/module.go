package httpclient

import (
	"net/http"

	"go.uber.org/fx"
)

// Module provides an *http.Client built from Options to the Fx graph.
var Module = fx.Module("httpclient",
	fx.Provide(newModuleClient),
)

// newModuleClient builds the client and closes its idle connections on shutdown.
func newModuleClient(lc fx.Lifecycle, opts Options) *http.Client {
	client := NewClient(opts)
	lc.Append(fx.StopHook(client.CloseIdleConnections))

	return client
}
