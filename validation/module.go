package validation

import "go.uber.org/fx"

// Module provides *Validator to the Fx graph.
var Module = fx.Module("validation",
	fx.Provide(NewValidator),
)
