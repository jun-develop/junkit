package i18n

import "go.uber.org/fx"

// Module provides *Translator to the Fx graph.
var Module = fx.Module("i18n",
	fx.Provide(NewTranslator),
)
