package nodebridge

import (
	"context"
	"sync"
	"time"

	grpcretry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpcprometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/core/events"
	"github.com/iotaledger/hive.go/core/logger"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	supportedProtocolVersion = 2
)

type NodeBridge struct {
	// the logger used to log events.
	*logger.WrappedLogger

	conn       *grpc.ClientConn
	client     inx.INXClient
	NodeConfig *inx.NodeConfiguration

	Events *Events

	nodeStatusMutex    sync.RWMutex
	nodeStatus         *inx.NodeStatus
	protocolParameters *iotago.ProtocolParameters
}

type Events struct {
	LatestMilestoneChanged    *events.Event
	ConfirmedMilestoneChanged *events.Event
}

func MilestoneCaller(handler interface{}, params ...interface{}) {
	//nolint:forcetypeassert // we will replace that with generic events anyway
	handler.(func(metadata *Milestone))(params[0].(*Milestone))
}

func NewNodeBridge(ctx context.Context, address string, log *logger.Logger) (*NodeBridge, error) {
	conn, err := grpc.Dial(address,
		grpc.WithChainUnaryInterceptor(grpcretry.UnaryClientInterceptor(), grpcprometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpcprometheus.StreamClientInterceptor),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	client := inx.NewINXClient(conn)
	retryBackoff := func(_ uint) time.Duration {
		return 1 * time.Second
	}

	log.Info("Connecting to node and reading node configuration...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpcretry.WithMax(5), grpcretry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	log.Info("Reading node status...")
	nodeStatus, err := client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	protoParams, err := protocolParametersFromRaw(nodeStatus.GetCurrentProtocolParameters())
	if err != nil {
		return nil, err
	}

	return &NodeBridge{
		WrappedLogger: logger.NewWrappedLogger(log),
		conn:          conn,
		client:        client,
		NodeConfig:    nodeConfig,
		Events: &Events{
			LatestMilestoneChanged:    events.NewEvent(MilestoneCaller),
			ConfirmedMilestoneChanged: events.NewEvent(MilestoneCaller),
		},
		nodeStatus:         nodeStatus,
		protocolParameters: protoParams,
	}, nil
}

func (n *NodeBridge) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := n.listenToNodeStatus(c, cancel); err != nil {
			n.LogErrorf("Error listening to node status: %s", err)
		}
	}()

	<-c.Done()
	n.conn.Close()
}

func (n *NodeBridge) Client() inx.INXClient {
	return n.client
}
