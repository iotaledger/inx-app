package nodebridge

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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
		CommitmentID: ms.GetCommitmentInfo().GetCommitmentId().Unwrap(),
		Commitment:   commitment,
	}, nil
}

func (n *NodeBridge) LatestCommitment() (*Commitment, error) {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return commitmentFromINXCommitment(n.nodeStatus.GetLatestCommitment(), n.api)
}

func (n *NodeBridge) LatestCommittedSlotIndex() iotago.SlotIndex {
	latestCommitment, err := n.LatestCommitment()
	if err != nil || latestCommitment == nil {
		return 0
	}

	return latestCommitment.Commitment.Index
}

func (n *NodeBridge) ConfirmedCommitment() (*Commitment, error) {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return commitmentFromINXCommitment(n.nodeStatus.GetConfirmedCommitment(), n.api)
}

func (n *NodeBridge) ConfirmedSlotIndex() iotago.SlotIndex {
	confirmedCommitment, err := n.ConfirmedCommitment()
	if err != nil || confirmedCommitment == nil {
		return 0
	}

	return confirmedCommitment.Commitment.Index
}

func (n *NodeBridge) Commitment(ctx context.Context, index uint32) (*Commitment, error) {
	req := &inx.CommitmentRequest{
		CommitmentIndex: index,
	}
	ms, err := n.client.ReadCommitment(ctx, req)
	if err != nil {
		return nil, err
	}

	return commitmentFromINXCommitment(ms, n.api)
}

func (n *NodeBridge) CommitmentConeMetadata(ctx context.Context, cancel context.CancelFunc, index uint32, consumer func(metadata *inx.BlockMetadata)) error {
	defer cancel()

	req := &inx.CommitmentRequest{
		CommitmentIndex: index,
	}

	stream, err := n.client.ReadCommitmentConeMetadata(ctx, req)
	if err != nil {
		return err
	}

	for {
		metadata, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("ReadCommitmentConeMetadata: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}

		consumer(metadata)
	}

	//nolint:nilerr // false positive
	return nil
}
