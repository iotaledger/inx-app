package nodebridge

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	inx "github.com/iotaledger/inx/go"
	"github.com/iotaledger/iota.go/v3/nodeclient"
)

func (n *NodeBridge) INXNodeClient() *nodeclient.Client {
	return inx.NewNodeclientOverINX(n.client)
}

func (n *NodeBridge) RegisterAPIRoute(ctx context.Context, route string, bindAddress string) error {
	bindAddressParts := strings.Split(bindAddress, ":")
	if len(bindAddressParts) != 2 {
		return fmt.Errorf("invalid address %s", bindAddress)
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
	_, err = n.client.RegisterAPIRoute(ctx, apiReq)

	return err
}

func (n *NodeBridge) UnregisterAPIRoute(ctx context.Context, route string) error {
	apiReq := &inx.APIRouteRequest{
		Route: route,
	}
	_, err := n.client.UnregisterAPIRoute(ctx, apiReq)

	return err
}
