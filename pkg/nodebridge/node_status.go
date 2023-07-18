package nodebridge

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

const (
	ListenToNodeStatusCooldownInMilliseconds = 1_000
)

func (n *NodeBridge) NodeStatus() *inx.NodeStatus {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return n.nodeStatus
}

func (n *NodeBridge) IsNodeHealthy() bool {
	return n.NodeStatus().GetIsHealthy()
}

func (n *NodeBridge) IsNodeSynced() bool {
	return n.NodeStatus().GetIsSynced()
}

func (n *NodeBridge) IsNodeAlmostSynced() bool {
	return n.NodeStatus().GetIsAlmostSynced()
}

func (n *NodeBridge) LatestCommitment() (*iotago.Commitment, error) {
	//TODO: use the api for the correct version
	return n.NodeStatus().GetLatestCommitment().UnwrapCommitment(n.api)
}

func (n *NodeBridge) LatestFinalizedSlot() iotago.SlotIndex {
	return iotago.SlotIndex(n.NodeStatus().GetLatestFinalizedSlot())
}

func (n *NodeBridge) listenToNodeStatus(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	stream, err := n.client.ListenToNodeStatus(ctx, &inx.NodeStatusRequest{CooldownInMilliseconds: ListenToNodeStatusCooldownInMilliseconds})
	if err != nil {
		return err
	}

	for {
		nodeStatus, err := stream.Recv()
		if err != nil {
			if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("listenToNodeStatus: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}

		if err := n.processNodeStatus(nodeStatus); err != nil {
			n.LogErrorf("processNodeStatus: %s", err.Error())
			break
		}
	}

	//nolint:nilerr // false positive
	return nil
}

func (n *NodeBridge) processNodeStatus(nodeStatus *inx.NodeStatus) error {
	var latestCommitmentChanged bool
	var latestFinalizedSlotChanged bool

	updateStatus := func() error {
		n.nodeStatusMutex.Lock()
		defer n.nodeStatusMutex.Unlock()
		if nodeStatus.GetLatestCommitment().GetCommitmentId().Unwrap().Index() > n.nodeStatus.GetLatestCommitment().GetCommitmentId().Unwrap().Index() {
			latestCommitmentChanged = true
		}
		if nodeStatus.GetLatestFinalizedSlot() > n.nodeStatus.GetLatestFinalizedSlot() {
			latestFinalizedSlotChanged = true
		}
		n.nodeStatus = nodeStatus

		return nil
	}

	if err := updateStatus(); err != nil {
		return err
	}

	if latestCommitmentChanged {
		//TODO: use the api for the correct version
		commitment, err := commitmentFromINXCommitment(nodeStatus.GetLatestCommitment(), n.api)
		if err == nil {
			n.Events.LatestCommittedSlotChanged.Trigger(commitment)
		}
	}

	if latestFinalizedSlotChanged {
		n.Events.LatestFinalizedSlotChanged.Trigger(iotago.SlotIndex(nodeStatus.GetLatestFinalizedSlot()))
	}

	return nil
}
