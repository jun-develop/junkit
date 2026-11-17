package cache

import "go.uber.org/fx"

// Module provides a shared *Store to the Fx graph.
var Module = fx.Module("cache",
	fx.Provide(newModuleStore),
)

// newModuleStore builds the app's Store from opts and closes it when the app stops.
func newModuleStore(lc fx.Lifecycle, opts Options) *Store {
	s := NewStore(opts)
	lc.Append(fx.StopHook(s.Close))

	return s
}
