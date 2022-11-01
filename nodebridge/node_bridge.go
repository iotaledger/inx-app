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

	"github.com/iotaledger/hive.go/core/events"
	"github.com/iotaledger/hive.go/core/generics/options"
	"github.com/iotaledger/hive.go/core/logger"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
	"github.com/iotaledger/iota.go/v3/nodeclient"
)

const (
	supportedProtocolVersion = 2
)

type NodeBridge struct {
	// the logger used to log events.
	*logger.WrappedLogger

	targetNetworkName string

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

// WithTargetNetworkName checks if the network name of the node is equal to the given targetNetworkName.
// If targetNetworkName is empty, the check is disabled.
func WithTargetNetworkName(targetNetworkName string) options.Option[NodeBridge] {
	return func(n *NodeBridge) {
		n.targetNetworkName = targetNetworkName
	}
}

func NewNodeBridge(ctx context.Context, address string, maxConnectionAttempts uint, log *logger.Logger, opts ...options.Option[NodeBridge]) (*NodeBridge, error) {
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
		log.Info("> retrying INX connection to node ...")
		return 1 * time.Second
	}

	log.Info("Connecting to node and reading node configuration ...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpcretry.WithMax(maxConnectionAttempts), grpcretry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	log.Info("Reading node status ...")
	nodeStatus, err := client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	protoParams, err := protocolParametersFromRaw(nodeStatus.GetCurrentProtocolParameters())
	if err != nil {
		return nil, err
	}

	nb := options.Apply(&NodeBridge{
		WrappedLogger:     logger.NewWrappedLogger(log),
		targetNetworkName: "",
		conn:              conn,
		client:            client,
		NodeConfig:        nodeConfig,
		Events: &Events{
			LatestMilestoneChanged:    events.NewEvent(MilestoneCaller),
			ConfirmedMilestoneChanged: events.NewEvent(MilestoneCaller),
		},
		nodeStatus:         nodeStatus,
		protocolParameters: protoParams,
	}, opts)

	if nb.targetNetworkName != "" {
		// we need to check for the correct target network name
		if nb.targetNetworkName != protoParams.NetworkName {
			return nil, fmt.Errorf("network name mismatch, networkName: \"%s\", targetNetworkName: \"%s\"", protoParams.NetworkName, nb.targetNetworkName)
		}
	}

	return nb, nil
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
