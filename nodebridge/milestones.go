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

func (n *NodeBridge) IsNodeSynced() bool {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()

	if n.confirmedMilestone == nil || n.latestMilestone == nil {
		return false
	}

	return n.latestMilestone.GetMilestoneInfo().GetMilestoneIndex() == n.confirmedMilestone.GetMilestoneInfo().GetMilestoneIndex()
}

func (n *NodeBridge) LatestMilestone() (*iotago.Milestone, error) {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()
	return n.latestMilestone.UnwrapMilestone(serializer.DeSeriModeNoValidation, nil)
}

func (n *NodeBridge) ConfirmedMilestone() (*iotago.Milestone, error) {
	n.isSyncedMutex.RLock()
	defer n.isSyncedMutex.RUnlock()
	return n.confirmedMilestone.UnwrapMilestone(serializer.DeSeriModeNoValidation, nil)
}

func (n *NodeBridge) Milestone(index uint32) (*iotago.Milestone, error) {
	req := &inx.MilestoneRequest{
		MilestoneIndex: index,
	}
	m, err := n.client.ReadMilestone(context.Background(), req)
	if err != nil {
		return nil, err
	}
	return m.UnwrapMilestone(serializer.DeSeriModeNoValidation, nil)
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
		milestone, err := stream.Recv()
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
		n.processConfirmedMilestone(milestone)
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
		n.Events.LatestMilestoneChanged.Trigger(ms)
	}
}

func (n *NodeBridge) processConfirmedMilestone(ms *inx.Milestone) {
	var changed bool
	n.isSyncedMutex.Lock()
	if ms.GetMilestoneInfo().GetMilestoneIndex() > n.confirmedMilestone.GetMilestoneInfo().GetMilestoneIndex() {
		n.confirmedMilestone = ms
		changed = true
	}
	n.isSyncedMutex.Unlock()

	if changed {
		n.Events.ConfirmedMilestoneChanged.Trigger(ms)
	}
}
