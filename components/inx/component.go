package inx

import (
	"context"

	"go.uber.org/dig"

	"github.com/iotaledger/hive.go/app"
	"github.com/iotaledger/hive.go/app/shutdown"
	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/inx-app/pkg/nodebridge"
)

const PriorityDisconnectINX = 0

func init() {
	Component = &app.Component{
		Name:     "INX",
		DepsFunc: func(cDeps dependencies) { deps = cDeps },
		Params:   params,
		Provide:  provide,
		Run:      run,
	}
}

type dependencies struct {
	dig.In
	NodeBridge      nodebridge.NodeBridge
	ShutdownHandler *shutdown.ShutdownHandler
}

var (
	Component *app.Component
	deps      dependencies
)

func provide(c *dig.Container) error {
	return c.Provide(func() (nodebridge.NodeBridge, error) {
		nodeBridge := nodebridge.New(
			Component.Logger,
			nodebridge.WithTargetNetworkName(ParamsINX.TargetNetworkName),
		)

		if err := nodeBridge.Connect(
			Component.Daemon().ContextStopped(),
			ParamsINX.Address,
			ParamsINX.MaxConnectionAttempts,
		); err != nil {
			return nil, err
		}

		return nodeBridge, nil
	})
}

func run() error {
	return Component.Daemon().BackgroundWorker("INX", func(ctx context.Context) {
		Component.LogInfo("Starting NodeBridge ...")
		deps.NodeBridge.Run(ctx)
		Component.LogInfo("Stopped NodeBridge")

		if !ierrors.Is(ctx.Err(), context.Canceled) {
			deps.ShutdownHandler.SelfShutdown("INX connection to node dropped", true)
		}
	}, PriorityDisconnectINX)
}
