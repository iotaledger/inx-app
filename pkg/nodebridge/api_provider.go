package nodebridge

import iotago "github.com/iotaledger/iota.go/v4"

func (n *NodeBridge) APIForVersion(version iotago.Version) (iotago.API, error) {
	return n.api, nil
}

func (n *NodeBridge) APIForSlot(slot iotago.SlotIndex) iotago.API {
	return n.api
}

func (n *NodeBridge) APIForEpoch(epoch iotago.EpochIndex) iotago.API {
	return n.api
}

func (n *NodeBridge) LatestAPI() iotago.API {
	return n.api
}
