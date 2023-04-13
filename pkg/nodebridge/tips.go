package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (n *NodeBridge) RequestTips(ctx context.Context, count uint32, allowSemiLazy bool) (iotago.BlockIDs, error) {
	tipsResponse, err := n.client.RequestTips(ctx, &inx.TipsRequest{Count: count, AllowSemiLazy: allowSemiLazy})
	if err != nil {
		return nil, err
	}

	return tipsResponse.UnwrapTips(), nil
}
