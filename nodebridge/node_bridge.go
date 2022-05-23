package nodebridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	grpc_retry "github.com/grpc-ecosystem/go-grpc-middleware/retry"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"

	"github.com/iotaledger/hive.go/logger"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

type NodeBridge struct {
	logger     *logger.Logger
	conn       *grpc.ClientConn
	client     inx.INXClient
	NodeConfig *inx.NodeConfiguration
}

func NewNodeBridge(ctx context.Context, address string, logger *logger.Logger) (*NodeBridge, error) {

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

	logger.Info("Connecting to node and reading protocol parameters...")
	nodeConfig, err := client.ReadNodeConfiguration(ctx, &inx.NoParams{}, grpc_retry.WithMax(5), grpc_retry.WithBackoff(retryBackoff))
	if err != nil {
		return nil, err
	}

	return &NodeBridge{
		logger:     logger,
		conn:       conn,
		client:     client,
		NodeConfig: nodeConfig,
	}, nil
}

func (n *NodeBridge) Client() inx.INXClient {
	return n.client
}

func (n *NodeBridge) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	<-c.Done()
	n.conn.Close()
}

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	return n.NodeConfig.UnwrapProtocolParameters()
}

func (n *NodeBridge) RegisterAPIRoute(route string, bindAddress string) error {
	bindAddressParts := strings.Split(bindAddress, ":")
	if len(bindAddressParts) != 2 {
		return fmt.Errorf("Invalid address %s", bindAddress)
	}
	port, err := strconv.ParseInt(bindAddressParts[1], 10, 32)
	if err != nil {
		return err
	}

	apiReq := &inx.APIRouteRequest{
		Route: route,
		Host:  bindAddressParts[0],
		Port:  uint32(port),
	}

	if err != nil {
		return err
	}
	_, err = n.client.RegisterAPIRoute(context.Background(), apiReq)
	return err
}

func (n *NodeBridge) UnregisterAPIRoute(route string) error {
	apiReq := &inx.APIRouteRequest{
		Route: route,
	}
	_, err := n.client.UnregisterAPIRoute(context.Background(), apiReq)
	return err
}
