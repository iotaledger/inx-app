package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/lo"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Commitment struct {
	CommitmentID iotago.CommitmentID
	Commitment   *iotago.Commitment
}

func commitmentFromINXCommitment(ms *inx.Commitment, api iotago.API) (*Commitment, error) {
	if ms == nil || ms.GetCommitment() == nil {
		//nolint:nilnil // nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil
	}
	commitment, err := ms.UnwrapCommitment(api)
	if err != nil {
		return nil, err
	}

	return &Commitment{
		CommitmentID: ms.GetCommitmentId().Unwrap(),
		Commitment:   commitment,
	}, nil
}

// ForceCommitUntil forces the node to commit until the given slot.
func (n *nodeBridge) ForceCommitUntil(ctx context.Context, slot iotago.SlotIndex) error {
	return lo.Return2(n.client.ForceCommitUntil(ctx, inx.WrapSlotIndex(slot)))
}

// Commitment returns the commitment for the given slot.
func (n *nodeBridge) Commitment(ctx context.Context, slot iotago.SlotIndex) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentSlot: uint32(slot),
	}
	ms, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	return commitmentFromINXCommitment(ms, n.apiProvider.APIForSlot(slot))
}

// CommitmentByID returns the commitment for the given commitment ID.
func (n *nodeBridge) CommitmentByID(ctx context.Context, id iotago.CommitmentID) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentId: inx.NewCommitmentId(id),
	}
	ms, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	return commitmentFromINXCommitment(ms, n.apiProvider.APIForSlot(id.Index()))
}
