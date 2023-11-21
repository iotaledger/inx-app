package nodebridge

import (
	"context"
	"strconv"
	"strings"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
)

// RegisterAPIRoute registers the given API route.
func (n *NodeBridge) RegisterAPIRoute(ctx context.Context, route string, bindAddress string, path string) error {
	bindAddressParts := strings.Split(bindAddress, ":")
	if len(bindAddressParts) != 2 {
		return ierrors.Errorf("invalid address %s", bindAddress)
	}
	port, err := strconv.ParseInt(bindAddressParts[1], 10, 32)
	if err != nil {
		return err
	}

	apiReq := &inx.APIRouteRequest{
		Route: route,
		Host:  bindAddressParts[0],
		Port:  uint32(port),
		Path:  path,
	}

	if err != nil {
		return err
	}
	_, err = n.client.RegisterAPIRoute(ctx, apiReq)

	return err
}

// UnregisterAPIRoute unregisters the given API route.
func (n *NodeBridge) UnregisterAPIRoute(ctx context.Context, route string) error {
	apiReq := &inx.APIRouteRequest{
		Route: route,
	}
	_, err := n.client.UnregisterAPIRoute(ctx, apiReq)

	return err
}
