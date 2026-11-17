package storage

import "go.uber.org/fx"

// Module provides the storage Service to the Fx graph.
var Module = fx.Module("storage",
	fx.Provide(NewService),
)

// NewService returns the local filesystem Service as the storage.Service interface.
func NewService(opts Options) (Service, error) {
	return NewLocalService(opts)
}
