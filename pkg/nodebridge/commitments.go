package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/lo"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

type Commitment struct {
	CommitmentID iotago.CommitmentID
	Commitment   *iotago.Commitment
}

func commitmentFromINXCommitment(inxCommitment *inx.Commitment, api iotago.API) (*Commitment, error) {
	if inxCommitment == nil || inxCommitment.GetCommitment() == nil {
		//nolint:nilnil // nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil
	}

	commitment, err := inxCommitment.UnwrapCommitment(api)
	if err != nil {
		return nil, ierrors.Wrapf(err, "unable to unwrap commitment %s", inxCommitment.GetCommitmentId().Unwrap())
	}

	return &Commitment{
		CommitmentID: inxCommitment.GetCommitmentId().Unwrap(),
		Commitment:   commitment,
	}, nil
}

// ForceCommitUntil forces the node to commit until the given slot.
func (n *nodeBridge) ForceCommitUntil(ctx context.Context, slot iotago.SlotIndex) error {
	return lo.Return2(n.client.ForceCommitUntil(ctx, inx.WrapSlotRequest(slot)))
}

// Commitment returns the commitment for the given slot.
func (n *nodeBridge) Commitment(ctx context.Context, slot iotago.SlotIndex) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentSlot: uint32(slot),
	}

	inxCommitment, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	return commitmentFromINXCommitment(inxCommitment, n.apiProvider.APIForSlot(slot))
}

// CommitmentByID returns the commitment for the given commitment ID.
func (n *nodeBridge) CommitmentByID(ctx context.Context, id iotago.CommitmentID) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentId: inx.NewCommitmentId(id),
	}

	inxCommitment, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	return commitmentFromINXCommitment(inxCommitment, n.apiProvider.APIForSlot(id.Index()))
}

// ListenToCommitments listens to commitments.
func (n *nodeBridge) ListenToCommitments(ctx context.Context, startSlot, endSlot iotago.SlotIndex, consumer func(commitment *Commitment, rawData []byte) error) error {
	req := &inx.SlotRangeRequest{
		StartSlot: uint32(startSlot),
		EndSlot:   uint32(endSlot),
	}

	stream, err := n.client.ListenToCommitments(ctx, req)
	if err != nil {
		return err
	}

	if err := ListenToStream(ctx, stream.Recv, func(inxCommitment *inx.Commitment) error {
		commitmentID := inxCommitment.GetCommitmentId().Unwrap()

		commitment, err := inxCommitment.UnwrapCommitment(n.apiProvider.APIForSlot(commitmentID.Slot()))
		if err != nil {
			return ierrors.Wrapf(err, "unable to unwrap commitment %s", commitmentID)
		}

		return consumer(&Commitment{
			CommitmentID: commitmentID,
			Commitment:   commitment,
		}, inxCommitment.GetCommitment().GetData())
	}); err != nil {
		n.LogErrorf("ListenToCommitments failed: %s", err.Error())
		return err
	}

	return nil
}
