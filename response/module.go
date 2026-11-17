package response

import "go.uber.org/fx"

// Module provides Responder to the Fx graph; it needs a *i18n.Translator (i18n.Module).
var Module = fx.Module("response",
	fx.Provide(NewResponder),
)
