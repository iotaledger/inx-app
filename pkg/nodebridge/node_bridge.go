package nodebridge

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	grpcretry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpcprometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/logger"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/options"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/nodeclient"
)

const (
	supportedProtocolVersion = 2
)

type NodeBridge struct {
	// the logger used to log events.
	*logger.WrappedLogger

	targetNetworkName string
	Events            *Events

	conn               *grpc.ClientConn
	client             inx.INXClient
	NodeConfig         *inx.NodeConfiguration
	nodeStatusMutex    sync.RWMutex
	nodeStatus         *inx.NodeStatus
	protocolParameters *iotago.ProtocolParameters
}

type Events struct {
	LatestMilestoneChanged    *event.Event1[*Milestone]
	ConfirmedMilestoneChanged *event.Event1[*Milestone]
}

// WithTargetNetworkName checks if the network name of the node is equal to the given targetNetworkName.
// If targetNetworkName is empty, the check is disabled.
func WithTargetNetworkName(targetNetworkName string) options.Option[NodeBridge] {
	return func(n *NodeBridge) {
		n.targetNetworkName = targetNetworkName
	}
}

func NewNodeBridge(log *logger.Logger, opts ...options.Option[NodeBridge]) *NodeBridge {
	return options.Apply(&NodeBridge{
		WrappedLogger:     logger.NewWrappedLogger(log),
		targetNetworkName: "",
		Events: &Events{
			LatestMilestoneChanged:    event.New1[*Milestone](),
			ConfirmedMilestoneChanged: event.New1[*Milestone](),
		},
	}, opts)
}

func (n *NodeBridge) Connect(ctx context.Context, address string, maxConnectionAttempts uint) error {
	conn, err := grpc.Dial(address,
		grpc.WithChainUnaryInterceptor(grpcretry.UnaryClientInterceptor(), grpcprometheus.UnaryClientInterceptor),
		grpc.WithStreamInterceptor(grpcprometheus.StreamClientInterceptor),
		grpc.WithTransportCredentials(insecure.NewCredentials()),
	)
	if err != nil {
		return err
	}
	n.conn = conn
	n.client = inx.NewINXClient(conn)

	retryBackoff := func(_ uint) time.Duration {
		n.LogInfo("> retrying INX connection to node ...")
		return 1 * time.Second
	}

	n.LogInfo("Connecting to node and reading node configuration ...")
	nodeConfig, err := n.client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpcretry.WithMax(maxConnectionAttempts), grpcretry.WithBackoff(retryBackoff))
	if err != nil {
		return err
	}
	n.NodeConfig = nodeConfig

	n.LogInfo("Reading node status ...")
	nodeStatus, err := n.client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}
	n.nodeStatus = nodeStatus

	protoParams, err := protocolParametersFromRaw(nodeStatus.GetCurrentProtocolParameters())
	if err != nil {
		return err
	}
	n.protocolParameters = protoParams

	if n.targetNetworkName != "" {
		// we need to check for the correct target network name
		if n.targetNetworkName != protoParams.NetworkName {
			return fmt.Errorf("network name mismatch, networkName: \"%s\", targetNetworkName: \"%s\"", protoParams.NetworkName, n.targetNetworkName)
		}
	}

	return nil
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

// Indexer returns the IndexerClient.
// Returns ErrIndexerPluginNotAvailable if the current node does not support the plugin.
// It retries every second until the given context is done.
func (n *NodeBridge) Indexer(ctx context.Context) (nodeclient.IndexerClient, error) {

	nodeClient := n.INXNodeClient()

	getIndexerClient := func(ctx context.Context, nodeClient *nodeclient.Client) (nodeclient.IndexerClient, error) {
		ctxTimeout, cancelTimeout := context.WithTimeout(ctx, 1*time.Second)
		defer cancelTimeout()

		return nodeClient.Indexer(ctxTimeout)
	}

	// wait until indexer plugin is available
	for ctx.Err() == nil {
		indexer, err := getIndexerClient(ctx, nodeClient)
		if err != nil {
			if !errors.Is(err, nodeclient.ErrIndexerPluginNotAvailable) {
				return nil, err
			}
			time.Sleep(1 * time.Second)

			continue
		}

		return indexer, nil
	}

	return nil, nodeclient.ErrIndexerPluginNotAvailable
}

// EventAPI returns the EventAPIClient if supported by the node.
// Returns ErrMQTTPluginNotAvailable if the current node does not support the plugin.
// It retries every second until the given context is done.
func (n *NodeBridge) EventAPI(ctx context.Context) (*nodeclient.EventAPIClient, error) {

	nodeClient := n.INXNodeClient()

	getEventAPIClient := func(ctx context.Context, nodeClient *nodeclient.Client) (*nodeclient.EventAPIClient, error) {
		ctxTimeout, cancelTimeout := context.WithTimeout(ctx, 1*time.Second)
		defer cancelTimeout()

		return nodeClient.EventAPI(ctxTimeout)
	}

	// wait until Event API plugin is available
	for ctx.Err() == nil {
		eventAPIClient, err := getEventAPIClient(ctx, nodeClient)
		if err != nil {
			if !errors.Is(err, nodeclient.ErrMQTTPluginNotAvailable) {
				return nil, err
			}
			time.Sleep(1 * time.Second)

			continue
		}

		return eventAPIClient, nil
	}

	return nil, nodeclient.ErrMQTTPluginNotAvailable
}
