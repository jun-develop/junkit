package logger

import "go.uber.org/fx"

// Module provides *zap.Logger, built by New from a supplied Options, to the Fx graph.
var Module = fx.Module("logger",
	fx.Provide(New),
)
