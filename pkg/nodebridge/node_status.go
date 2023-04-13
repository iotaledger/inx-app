package nodebridge

import (
	"context"
	"errors"
	"fmt"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

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

func (n *NodeBridge) ProtocolParameters() *iotago.ProtocolParameters {
	n.nodeStatusMutex.RLock()
	defer n.nodeStatusMutex.RUnlock()

	return n.protocolParameters
}

func protocolParametersFromRaw(params *inx.RawProtocolParameters, api iotago.API) (*iotago.ProtocolParameters, error) {
	if params.ProtocolVersion != supportedProtocolVersion {
		return nil, fmt.Errorf("unsupported protocol version %d vs %d", params.ProtocolVersion, supportedProtocolVersion)
	}

	protoParams := &iotago.ProtocolParameters{}
	if _, err := api.Decode(params.GetParams(), &protoParams); err != nil {
		return nil, err
	}

	return protoParams, nil
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

	var latestCommittedSlotChanged bool
	var confirmedCommitmentChanged bool

	updateStatus := func() error {
		n.nodeStatusMutex.Lock()
		defer n.nodeStatusMutex.Unlock()
		if nodeStatus.GetLatestCommitment().GetCommitmentInfo().GetCommitmentIndex() > n.nodeStatus.GetLatestCommitment().GetCommitmentInfo().GetCommitmentIndex() {
			latestCommittedSlotChanged = true
		}
		if nodeStatus.GetConfirmedCommitment().GetCommitmentInfo().GetCommitmentIndex() > n.nodeStatus.GetConfirmedCommitment().GetCommitmentInfo().GetCommitmentIndex() {
			confirmedCommitmentChanged = true
		}
		n.nodeStatus = nodeStatus

		protocolParams, err := protocolParametersFromRaw(nodeStatus.GetCurrentProtocolParameters(), n.api)
		if err != nil {
			return err
		}
		n.protocolParameters = protocolParams

		return nil
	}

	if err := updateStatus(); err != nil {
		return err
	}

	if latestCommittedSlotChanged {
		commitment, err := commitmentFromINXCommitment(nodeStatus.GetLatestCommitment(), n.api)
		if err == nil {
			n.Events.LatestCommittedSlotChanged.Trigger(commitment)
		}
	}

	if confirmedCommitmentChanged {
		commitment, err := commitmentFromINXCommitment(nodeStatus.GetConfirmedCommitment(), n.api)
		if err == nil {
			n.Events.ConfirmedSlotChanged.Trigger(commitment)
		}
	}

	return nil
}
