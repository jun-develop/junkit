package notification

import (
	"go.uber.org/fx"
	"go.uber.org/zap"

	"github.com/jun-develop/junkit/telegram"
)

// Module provides Notifier to the Fx graph; its queue is drained and stopped OnStop.
var Module = fx.Module("notification",
	fx.Provide(newModuleNotifier),
)

// moduleParams are Module's dependencies.
type moduleParams struct {
	fx.In

	Lifecycle fx.Lifecycle
	Options   Options
	Telegram  telegram.Client
	Logger    *zap.Logger `optional:"true"`
}

// newModuleNotifier builds the Notifier Module provides and closes it OnStop.
func newModuleNotifier(p moduleParams) Notifier {
	n := NewNotifier(p.Options, p.Telegram, p.Logger)
	p.Lifecycle.Append(fx.Hook{OnStop: n.Close})

	return n
}
