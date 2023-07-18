package nodebridge

import (
	"context"
	"sync"
	"time"

	grpcretry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpcprometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	"github.com/iotaledger/hive.go/logger"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/options"
	"github.com/iotaledger/inx-app/pkg/api"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/nodeclient"
)

type NodeBridge struct {
	// the logger used to log events.
	*logger.WrappedLogger
	api iotago.API

	targetNetworkName string
	Events            *Events

	conn        *grpc.ClientConn
	client      inx.INXClient
	NodeConfig  *inx.NodeConfiguration
	apiProvider *api.DynamicMockAPIProvider

	nodeStatusMutex sync.RWMutex
	nodeStatus      *inx.NodeStatus
}

type Events struct {
	LatestCommittedSlotChanged *event.Event1[*Commitment]
	LatestFinalizedSlotChanged *event.Event1[iotago.SlotIndex]
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
			LatestCommittedSlotChanged: event.New1[*Commitment](),
			LatestFinalizedSlotChanged: event.New1[iotago.SlotIndex](),
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

	n.apiProvider = api.NewDynamicMockAPIProvider()
	for _, rawParams := range n.NodeConfig.ProtocolParameters {
		protoParams, err := lo.DropCount(iotago.ProtocolParametersFromBytes(rawParams.GetParams()))
		if err != nil {
			return err
		}
		n.apiProvider.AddProtocolParameters(iotago.EpochIndex(rawParams.GetStartEpoch()), protoParams)
	}

	if n.targetNetworkName != "" {
		// we need to check for the correct target network name
		if n.targetNetworkName != n.APIProvider().CurrentAPI().ProtocolParameters().NetworkName() {
			return ierrors.Errorf("network name mismatch, networkName: \"%s\", targetNetworkName: \"%s\"", n.APIProvider().CurrentAPI().ProtocolParameters().NetworkName(), n.targetNetworkName)
		}
	}

	n.LogInfo("Reading node status ...")
	nodeStatus, err := n.client.ReadNodeStatus(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}
	n.nodeStatus = nodeStatus

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
	_ = n.conn.Close()
}

func (n *NodeBridge) Client() inx.INXClient {
	return n.client
}

func (n *NodeBridge) APIProvider() api.Provider {
	return n.apiProvider
}

// Indexer returns the IndexerClient.
// Returns ErrIndexerPluginNotAvailable if the current node does not support the plugin.
// It retries every second until the given context is done.
func (n *NodeBridge) Indexer(ctx context.Context) (nodeclient.IndexerClient, error) {

	nodeClient, err := n.INXNodeClient()
	if err != nil {
		return nil, err
	}

	getIndexerClient := func(ctx context.Context, nodeClient *nodeclient.Client) (nodeclient.IndexerClient, error) {
		ctxTimeout, cancelTimeout := context.WithTimeout(ctx, 1*time.Second)
		defer cancelTimeout()

		return nodeClient.Indexer(ctxTimeout)
	}

	// wait until indexer plugin is available
	for ctx.Err() == nil {
		indexer, err := getIndexerClient(ctx, nodeClient)
		if err != nil {
			if !ierrors.Is(err, nodeclient.ErrIndexerPluginNotAvailable) {
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

	nodeClient, err := n.INXNodeClient()
	if err != nil {
		return nil, err
	}

	getEventAPIClient := func(ctx context.Context, nodeClient *nodeclient.Client) (*nodeclient.EventAPIClient, error) {
		ctxTimeout, cancelTimeout := context.WithTimeout(ctx, 1*time.Second)
		defer cancelTimeout()

		return nodeClient.EventAPI(ctxTimeout)
	}

	// wait until Event API plugin is available
	for ctx.Err() == nil {
		eventAPIClient, err := getEventAPIClient(ctx, nodeClient)
		if err != nil {
			if !ierrors.Is(err, nodeclient.ErrMQTTPluginNotAvailable) {
				return nil, err
			}
			time.Sleep(1 * time.Second)

			continue
		}

		return eventAPIClient, nil
	}

	return nil, nodeclient.ErrMQTTPluginNotAvailable
}
