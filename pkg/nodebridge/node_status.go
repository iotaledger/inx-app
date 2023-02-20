package nodebridge

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

const (
	ListenToNodeStatusCooldownInMilliseconds = 1_000
)

func (n *NodeBridge) NodeStatus() (*inx.NodeStatus, error) {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	if n.nodeStatusCtx != nil && n.nodeStatusCtx.Err() != nil {
		return nil, n.nodeStatusCtx.Err()
	}

	return n.nodeStatus, nil
}

func (n *NodeBridge) IsNodeHealthy() (bool, error) {
	nodeStatus, err := n.NodeStatus()
	if err != nil {
		return false, err
	}

	return nodeStatus.GetIsHealthy(), nil
}

func (n *NodeBridge) IsNodeSynced() (bool, error) {
	nodeStatus, err := n.NodeStatus()
	if err != nil {
		return false, err
	}

	return nodeStatus.GetIsSynced(), nil
}

func (n *NodeBridge) IsNodeAlmostSynced() (bool, error) {
	nodeStatus, err := n.NodeStatus()
	if err != nil {
		return false, err
	}

	return nodeStatus.GetIsAlmostSynced(), nil
}

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return n.protocolParameters
}

func protocolParametersFromRaw(params *inx.RawProtocolParameters) (*iotago.ProtocolParameters, error) {
	if params.ProtocolVersion != supportedProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version %d vs %d", params.ProtocolVersion, supportedProtocolVersion)
	}

	protoParams := &iotago.ProtocolParameters{}
	if _, err := protoParams.Deserialize(params.GetParams(), serializer.DeSeriModeNoValidation, nil); err != nil {
		return nil, err
	}

	return protoParams, nil
}

func (n *NodeBridge) listenToNodeStatus(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	// set the context, so we can track when listening to node status updates was stopped
	n.nodeStatusCtx = ctx

	stream, err := n.client.ListenToNodeStatus(ctx, &inx.NodeStatusRequest{CooldownInMilliseconds: ListenToNodeStatusCooldownInMilliseconds})
	if err != nil {
		return err
	}

	for {
		nodeStatus, err := stream.Recv()
		if err != nil {
			if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
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

	var latestMilestoneChanged bool
	var confirmedMilestoneChanged bool

	updateStatus := func() error {
		n.nodeStatusMutex.Lock()
		defer n.nodeStatusMutex.Unlock()

		if nodeStatus.GetLatestMilestone().GetMilestoneInfo().GetMilestoneIndex() > n.nodeStatus.GetLatestMilestone().GetMilestoneInfo().GetMilestoneIndex() {
			latestMilestoneChanged = true
		}

		if nodeStatus.GetConfirmedMilestone().GetMilestoneInfo().GetMilestoneIndex() > n.nodeStatus.GetConfirmedMilestone().GetMilestoneInfo().GetMilestoneIndex() {
			confirmedMilestoneChanged = true
		}
		n.nodeStatus = nodeStatus

		protocolParams, err := protocolParametersFromRaw(nodeStatus.GetCurrentProtocolParameters())
		if err != nil {
			return err
		}
		n.protocolParameters = protocolParams

		return nil
	}

	if err := updateStatus(); err != nil {
		return err
	}

	if latestMilestoneChanged {
		milestone, err := milestoneFromINXMilestone(nodeStatus.GetLatestMilestone())
		if err == nil {
			n.Events.LatestMilestoneChanged.Trigger(milestone)
		}
	}

	if confirmedMilestoneChanged {
		milestone, err := milestoneFromINXMilestone(nodeStatus.GetConfirmedMilestone())
		if err == nil {
			n.Events.ConfirmedMilestoneChanged.Trigger(milestone)
		}
	}

	return nil
}
