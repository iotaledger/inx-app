package nodebridge

import (
	"context"
	"fmt"
	"sync"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/core/events"
	"github.com/iotaledger/hive.go/core/logger"
	"github.com/iotaledger/hive.go/serializer/v2"
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

	isSyncedMutex      sync.RWMutex
	latestMilestone    *inx.Milestone
	confirmedMilestone *inx.Milestone
	protocolParameters *iotago.ProtocolParameters
}

type Events struct {
	LatestMilestoneChanged    *events.Event
	ConfirmedMilestoneChanged *events.Event
}

func INXMilestoneCaller(handler interface{}, params ...interface{}) {
	//nolint:forcetypeassert // we will replace that with generic events anyway
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

	log.Info("Connecting to node and reading node configuration...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpc_retry.WithMax(5), grpc_retry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	log.Info("Reading node status...")
	nodeStatus, err := client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	log.Info("Reading protocol parameters...")
	params, err := client.ReadProtocolParameters(ctx, &inx.MilestoneRequest{MilestoneIndex: nodeStatus.GetConfirmedMilestone().GetMilestoneInfo().GetMilestoneIndex()})
	if err != nil {
		return nil, err
	}

	protoParams, err := protocolParametersFromRaw(params)
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
		protocolParameters: protoParams,
	}, nil
}

func (n *NodeBridge) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := n.listenToConfirmedMilestones(c, cancel); err != nil {
			n.LogErrorf("Error listening to confirmed milestones: %s", err)
		}
	}()

	go func() {
		if err := n.listenToLatestMilestones(c, cancel); err != nil {
			n.LogErrorf("Error listening to latest milestones: %s", err)
		}
	}()

	<-c.Done()
	n.conn.Close()
}

func protocolParametersFromRaw(params *inx.RawProtocolParameters) (*iotago.ProtocolParameters, error) {
	if params.ProtocolVersion != supportedProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version %d vs %d", params.ProtocolVersion, supportedProtocolVersion)
	}

	protoParams := &iotago.ProtocolParameters{}
	if _, err := protoParams.Deserialize(params.GetParams(), serializer.DeSeriModeNoValidation, nil); err != nil {
		return nil, err
	}

	return protoParams, nil
}

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	return n.protocolParameters
}

func (n *NodeBridge) Client() inx.INXClient {
	return n.client
}

func (n *NodeBridge) NodeStatus() (*inx.NodeStatus, error) {
	s, err := n.client.ReadNodeStatus(context.Background(), &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	return s, nil
}
