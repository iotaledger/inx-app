package nodebridge

import (
	"context"
	"errors"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

type Milestone struct {
	MilestoneID iotago.MilestoneID
	Milestone   *iotago.Milestone
}

func milestoneFromINXMilestone(ms *inx.Milestone) (*Milestone, error) {
	if ms == nil || ms.GetMilestone() == nil {
		//nolint:nilnil // nil, nil is ok in this context, even if it is not go idiomatic
		return nil, nil
	}
	milestone, err := ms.UnwrapMilestone(serializer.DeSeriModeNoValidation, nil)
	if err != nil {
		return nil, err
	}

	return &Milestone{
		MilestoneID: ms.GetMilestoneInfo().GetMilestoneId().Unwrap(),
		Milestone:   milestone,
	}, nil
}

func (n *NodeBridge) LatestMilestone() (*Milestone, error) {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return milestoneFromINXMilestone(n.nodeStatus.GetLatestMilestone())
}

func (n *NodeBridge) LatestMilestoneIndex() uint32 {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	// access the milestone info directly,
	// in case the index is known but not the milestone itself (node bootstrap)
	ms := n.nodeStatus.GetLatestMilestone()
	if ms == nil {
		return 0
	}

	msInfo := ms.GetMilestoneInfo()
	if msInfo == nil {
		return 0
	}

	return msInfo.MilestoneIndex
}

func (n *NodeBridge) ConfirmedMilestone() (*Milestone, error) {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return milestoneFromINXMilestone(n.nodeStatus.GetConfirmedMilestone())
}

func (n *NodeBridge) ConfirmedMilestoneIndex() uint32 {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	// access the milestone info directly,
	// in case the index is known but not the milestone itself (node bootstrap)
	ms := n.nodeStatus.GetConfirmedMilestone()
	if ms == nil {
		return 0
	}

	msInfo := ms.GetMilestoneInfo()
	if msInfo == nil {
		return 0
	}

	return msInfo.MilestoneIndex
}

func (n *NodeBridge) Milestone(ctx context.Context, index uint32) (*Milestone, error) {
	req := &inx.MilestoneRequest{
		MilestoneIndex: index,
	}
	ms, err := n.client.ReadMilestone(ctx, req)
	if err != nil {
		return nil, err
	}

	return milestoneFromINXMilestone(ms)
}

func (n *NodeBridge) MilestoneConeMetadata(ctx context.Context, cancel context.CancelFunc, index uint32, consumer func(metadata *inx.BlockMetadata)) error {
	defer cancel()

	req := &inx.MilestoneRequest{
		MilestoneIndex: index,
	}

	stream, err := n.client.ReadMilestoneConeMetadata(ctx, req)
	if err != nil {
		return err
	}

	for {
		metadata, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("ReadMilestoneConeMetadata: %s", err.Error())

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
