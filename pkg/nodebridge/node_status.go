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

func (n *NodeBridge) LatestCommitment() *Commitment {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return n.latestCommitment
}

func (n *NodeBridge) LatestFinalizedCommitment() *Commitment {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return n.latestFinalizedCommitment
}

func (n *NodeBridge) PruningEpoch() iotago.EpochIndex {
	return iotago.EpochIndex(n.NodeStatus().GetPruningEpoch())
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

	var latestCommitment *Commitment
	var latestCommitmentChanged bool

	var latestFinalizedCommitment *Commitment
	var latestFinalizedCommitmentChanged bool

	updateStatus := func() error {
		n.nodeStatusMutex.Lock()
		defer n.nodeStatusMutex.Unlock()
		var err error

		if n.nodeStatus == nil || nodeStatus.GetLatestCommitment().GetCommitmentId().Unwrap().Slot() > n.nodeStatus.GetLatestCommitment().GetCommitmentId().Unwrap().Slot() {
			if latestCommitment, err = commitmentFromINXCommitment(nodeStatus.GetLatestCommitment(), n.apiProvider.CommittedAPI()); err == nil {
				n.latestCommitment = latestCommitment
				latestCommitmentChanged = true
			}
		}
		if n.nodeStatus == nil || nodeStatus.GetLatestFinalizedCommitment().GetCommitmentId().Unwrap().Slot() > n.nodeStatus.GetLatestFinalizedCommitment().GetCommitmentId().Unwrap().Slot() {
			if latestFinalizedCommitment, err = commitmentFromINXCommitment(nodeStatus.GetLatestFinalizedCommitment(), n.apiProvider.CommittedAPI()); err == nil {
				n.latestFinalizedCommitment = latestFinalizedCommitment
				latestFinalizedCommitmentChanged = true
			}
		}
		n.nodeStatus = nodeStatus

		return nil
	}

	if err := updateStatus(); err != nil {
		return err
	}

	if latestCommitmentChanged {
		slot := latestCommitment.CommitmentID.Slot()
		n.apiProvider.SetCommittedSlot(slot)

		n.Events.LatestCommitmentChanged.Trigger(latestCommitment)
	}

	if latestFinalizedCommitmentChanged {
		n.Events.LatestFinalizedCommitmentChanged.Trigger(latestFinalizedCommitment)
	}

	return nil
}
