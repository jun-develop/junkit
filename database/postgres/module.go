package postgres

import (
	"context"

	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/database"
)

// Module provides *DB to the Fx graph.
var Module = fx.Module("postgres",
	fx.Provide(newFx),
)

// params are newFx's Fx-injected dependencies.
type params struct {
	fx.In

	Lifecycle fx.Lifecycle
	Options   Options
	Logger    *zap.Logger
}

// newFx creates a DB with New and ties it to the Fx lifecycle: ping on start, close on stop.
func newFx(p params) (*DB, error) {
	db, err := New(p.Options, p.Logger)
	if err != nil {
		return nil, err
	}

	p.Lifecycle.Append(fx.Hook{
		OnStart: func(ctx context.Context) error {
			if err := db.Ping(ctx); err != nil {
				return err
			}

			p.Logger.Info("postgres: connected",
				zap.String("name", p.Options.Name), zap.String("dsn", database.MaskDSN(p.Options.DSN)))

			return nil
		},
		OnStop: func(context.Context) error {
			p.Logger.Info("postgres: closing connections", zap.String("name", p.Options.Name))
			db.Close()

			return nil
		},
	})

	return db, nil
}
