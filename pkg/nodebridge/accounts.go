package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (n *NodeBridge) ReadIsCandidate(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsCandidate(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}

func (n *NodeBridge) ReadIsCommitteeMember(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsCommitteeMember(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}
func (n *NodeBridge) ReadIsValidatorAccount(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsValidatorAccount(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}
