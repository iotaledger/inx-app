package inx

import (
	"context"

	"github.com/pkg/errors"
	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/core/app"
	"github.com/iotaledger/hive.go/core/app/core/shutdown"
	"github.com/iotaledger/inx-app/nodebridge"
)

const PriorityDisconnectINX = 0

func init() {
	CoreComponent = &app.CoreComponent{
		Component: &app.Component{
			Name:     "INX",
			DepsFunc: func(cDeps dependencies) { deps = cDeps },
			Params:   params,
			Provide:  provide,
			Run:      run,
		},
	}
}

type dependencies struct {
	dig.In
	NodeBridge      *nodebridge.NodeBridge
	ShutdownHandler *shutdown.ShutdownHandler
}

var (
	CoreComponent *app.CoreComponent
	deps          dependencies
)

func provide(c *dig.Container) error {
	return c.Provide(func() (*nodebridge.NodeBridge, error) {
		return nodebridge.NewNodeBridge(CoreComponent.Daemon().ContextStopped(),
			ParamsINX.Address,
			CoreComponent.Logger())
	})
}

func run() error {
	return CoreComponent.Daemon().BackgroundWorker("INX", func(ctx context.Context) {
		CoreComponent.LogInfo("Starting NodeBridge")
		deps.NodeBridge.Run(ctx)
		CoreComponent.LogInfo("Stopped NodeBridge")

		if !errors.Is(ctx.Err(), context.Canceled) {
			deps.ShutdownHandler.SelfShutdown("INX connection to node dropped", true)
		}
	}, PriorityDisconnectINX)
}
