package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/serializer/v2"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

func (n *NodeBridge) SubmitBlock(ctx context.Context, block *iotago.Block) (iotago.BlockID, error) {
	blk, err := inx.WrapBlock(block)
	if err != nil {
		return iotago.BlockID{}, err
	}

	response, err := n.client.SubmitBlock(ctx, blk)
	if err != nil {
		return iotago.BlockID{}, err
	}

	return response.Unwrap(), nil
}

func (n *NodeBridge) BlockMetadata(blockID iotago.BlockID) (*inx.BlockMetadata, error) {
	return n.client.ReadBlockMetadata(context.Background(), inx.NewBlockId(blockID))
}

func (n *NodeBridge) Block(blockID iotago.BlockID) (*iotago.Block, error) {
	inxMsg, err := n.client.ReadBlock(context.Background(), inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}
	return inxMsg.UnwrapBlock(serializer.DeSeriModeNoValidation, nil)
}
