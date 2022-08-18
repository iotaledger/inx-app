package nodebridge

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	isNodeAlmostSyncedThreshold = 2
)

type Milestone struct {
	MilestoneID iotago.MilestoneID
	Milestone   *iotago.Milestone
}

func milestoneFromINXMilestone(ms *inx.Milestone) (*Milestone, error) {
	if ms == nil || ms.GetMilestone() == nil {
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

func (n *NodeBridge) IsNodeSynced() bool {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	if n.latestMilestone == nil || n.confirmedMilestone == nil {
		return false
	}

	return n.latestMilestone.GetMilestoneInfo().GetMilestoneIndex() == n.confirmedMilestone.GetMilestoneInfo().GetMilestoneIndex()
}

func (n *NodeBridge) IsNodeAlmostSynced() bool {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	if n.latestMilestone == nil || n.confirmedMilestone == nil {
		return false
	}

	return n.latestMilestone.GetMilestoneInfo().GetMilestoneIndex()-isNodeAlmostSyncedThreshold <= n.confirmedMilestone.GetMilestoneInfo().GetMilestoneIndex()
}

func (n *NodeBridge) LatestMilestone() (*Milestone, error) {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	return milestoneFromINXMilestone(n.latestMilestone)
}

func (n *NodeBridge) LatestMilestoneIndex() uint32 {
	latestMilestone, err := n.LatestMilestone()
	if err != nil || latestMilestone == nil {
		return 0
	}

	return latestMilestone.Milestone.Index
}

func (n *NodeBridge) ConfirmedMilestone() (*Milestone, error) {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	return milestoneFromINXMilestone(n.confirmedMilestone)
}

func (n *NodeBridge) ConfirmedMilestoneIndex() uint32 {
	confirmedMilestone, err := n.ConfirmedMilestone()
	if err != nil || confirmedMilestone == nil {
		return 0
	}

	return confirmedMilestone.Milestone.Index
}

func (n *NodeBridge) Milestone(index uint32) (*Milestone, error) {
	req := &inx.MilestoneRequest{
		MilestoneIndex: index,
	}
	ms, err := n.client.ReadMilestone(context.Background(), req)
	if err != nil {
		return nil, err
	}

	return milestoneFromINXMilestone(ms)
}

func (n *NodeBridge) listenToLatestMilestones(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()
	stream, err := n.client.ListenToLatestMilestones(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}
	for {
		milestone, err := stream.Recv()
		if err != nil {
			if err == io.EOF || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("listenToLatestMilestones: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}
		n.processLatestMilestone(milestone)
	}

	return nil
}

func (n *NodeBridge) listenToConfirmedMilestones(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()
	stream, err := n.client.ListenToConfirmedMilestones(ctx, &inx.MilestoneRangeRequest{})
	if err != nil {
		return err
	}
	for {
		milestoneAndParams, err := stream.Recv()
		if err != nil {
			if err == io.EOF || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("listenToConfirmedMilestones: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}
		protoParams, err := protocolParametersFromRaw(milestoneAndParams.GetCurrentProtocolParameters())
		if err != nil {
			n.LogErrorf("listenToConfirmedMilestones: %s", err.Error())

			return err
		}

		n.processConfirmedMilestoneAndProtocolParameters(milestoneAndParams.GetMilestone(), protoParams)
	}

	return nil
}

func (n *NodeBridge) processLatestMilestone(ms *inx.Milestone) {
	var changed bool
	n.isSyncedMutex.Lock()
	if ms.GetMilestoneInfo().GetMilestoneIndex() > n.latestMilestone.GetMilestoneInfo().GetMilestoneIndex() {
		n.latestMilestone = ms
		changed = true
	}
	n.isSyncedMutex.Unlock()

	if changed {
		milestone, err := milestoneFromINXMilestone(ms)
		if err == nil {
			n.Events.LatestMilestoneChanged.Trigger(milestone)
		}
	}
}

func (n *NodeBridge) processConfirmedMilestoneAndProtocolParameters(ms *inx.Milestone, protoParams *iotago.ProtocolParameters) {
	var changed bool
	n.isSyncedMutex.Lock()
	if ms.GetMilestoneInfo().GetMilestoneIndex() > n.confirmedMilestone.GetMilestoneInfo().GetMilestoneIndex() {
		n.confirmedMilestone = ms
		n.protocolParameters = protoParams
		changed = true
	}
	n.isSyncedMutex.Unlock()

	if changed {
		milestone, err := milestoneFromINXMilestone(ms)
		if err == nil {
			n.Events.ConfirmedMilestoneChanged.Trigger(milestone)
		}
	}
}

func (n *NodeBridge) MilestoneConeMetadata(ctx context.Context, cancel context.CancelFunc, index uint32, consumer func(metadata *inx.BlockMetadata)) error {
	defer cancel()

	req := &inx.MilestoneRequest{
		MilestoneIndex: index,
	}

	stream, err := n.client.ReadMilestoneConeMetadata(context.Background(), req)
	if err != nil {
		return err
	}
	for {
		metadata, err := stream.Recv()
		if err != nil {
			if err == io.EOF || status.Code(err) == codes.Canceled {
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

	return nil
}
