package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (n *NodeBridge) RequestTips(ctx context.Context, count uint32) (strong iotago.BlockIDs, weak iotago.BlockIDs, shallowLike iotago.BlockIDs, err error) {
	tipsResponse, err := n.client.RequestTips(ctx, &inx.TipsRequest{Count: count})
	if err != nil {
		return nil, nil, nil, err
	}

	return tipsResponse.UnwrapStrongTips(), tipsResponse.UnwrapWeakTips(), tipsResponse.UnwrapShallowLikeTips(), nil
}
