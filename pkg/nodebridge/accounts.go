package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

// ReadIsCandidate returns true if the given account is a candidate.
func (n *nodeBridge) ReadIsCandidate(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsCandidate(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}

// ReadIsCommitteeMember returns true if the given account is a committee member.
func (n *nodeBridge) ReadIsCommitteeMember(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsCommitteeMember(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}

// ReadIsValidatorAccount returns true if the given account is a validator account.
func (n *nodeBridge) ReadIsValidatorAccount(ctx context.Context, id iotago.AccountID, slot iotago.SlotIndex) (bool, error) {
	result, err := n.client.ReadIsValidatorAccount(ctx, inx.NewAccountInfoRequest(id, slot))
	if err != nil {
		return false, err
	}

	return result.GetValue(), nil
}
