package nodebridge

import (
	"context"

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

func (n *NodeBridge) Commitment(ctx context.Context, index iotago.SlotIndex) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentIndex: uint64(index),
	}
	ms, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	//TODO: use api depending on version
	return commitmentFromINXCommitment(ms, n.api)
}

func (n *NodeBridge) CommitmentByID(ctx context.Context, id iotago.CommitmentID) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentId: inx.NewCommitmentId(id),
	}
	ms, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	//TODO: use api depending on version
	return commitmentFromINXCommitment(ms, n.api)
}
