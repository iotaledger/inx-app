package nodebridge

import (
	"context"
	"sync"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/events"
	"github.com/iotaledger/hive.go/logger"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

type NodeBridge struct {
	// the logger used to log events.
	*logger.WrappedLogger

	conn       *grpc.ClientConn
	client     inx.INXClient
	NodeConfig *inx.NodeConfiguration

	Events *Events

	isSyncedMutex      sync.RWMutex
	latestMilestone    *inx.Milestone
	confirmedMilestone *inx.Milestone
}

type Events struct {
	LatestMilestoneChanged    *events.Event
	ConfirmedMilestoneChanged *events.Event
}

func INXMilestoneCaller(handler interface{}, params ...interface{}) {
	handler.(func(metadata *Milestone))(params[0].(*Milestone))
}

func NewNodeBridge(ctx context.Context, address string, log *logger.Logger) (*NodeBridge, error) {
	conn, err := grpc.Dial(address,
		grpc.WithChainUnaryInterceptor(grpc_retry.UnaryClientInterceptor(), grpc_prometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return nil, err
	}
	client := inx.NewINXClient(conn)
	retryBackoff := func(_ uint) time.Duration {
		return 1 * time.Second
	}

	log.Info("Connecting to node and reading protocol parameters...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpc_retry.WithMax(5), grpc_retry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	log.Info("Reading node status...")
	nodeStatus, err := client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	return &NodeBridge{
		WrappedLogger: logger.NewWrappedLogger(log),
		conn:          conn,
		client:        client,
		NodeConfig:    nodeConfig,
		Events: &Events{
			LatestMilestoneChanged:    events.NewEvent(INXMilestoneCaller),
			ConfirmedMilestoneChanged: events.NewEvent(INXMilestoneCaller),
		},
		latestMilestone:    nodeStatus.GetLatestMilestone(),
		confirmedMilestone: nodeStatus.GetConfirmedMilestone(),
	}, nil
}

func (n *NodeBridge) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	go n.listenToConfirmedMilestones(c, cancel)
	go n.listenToLatestMilestones(c, cancel)
	<-c.Done()
	n.conn.Close()
}

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	return n.NodeConfig.UnwrapProtocolParameters()
}

func (n *NodeBridge) Client() inx.INXClient {
	return n.client
}

func (n *NodeBridge) NodeStatus() (*inx.NodeStatus, error) {
	s, err := n.client.ReadNodeStatus(context.Background(), &inx.NoParams{})
	if err != nil {
		return nil, err
	}
	n.processLatestMilestone(s.GetLatestMilestone())
	n.processConfirmedMilestone(s.GetConfirmedMilestone())
	return s, nil
}
