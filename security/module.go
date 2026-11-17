package security

import "go.uber.org/fx"

// Module provides TokenSealer and PasswordHasher to the Fx graph.
var Module = fx.Module("security",
	fx.Provide(NewTokenSealer, NewPasswordHasher),
)
