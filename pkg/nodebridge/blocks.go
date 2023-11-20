package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

func (n *NodeBridge) ActiveRootBlocks(ctx context.Context) (map[iotago.BlockID]iotago.CommitmentID, error) {
	response, err := n.client.ReadActiveRootBlocks(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	return response.Unwrap()
}

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

func (n *NodeBridge) BlockMetadata(ctx context.Context, blockID iotago.BlockID) (*inx.BlockMetadata, error) {
	return n.client.ReadBlockMetadata(ctx, inx.NewBlockId(blockID))
}

func (n *NodeBridge) Block(ctx context.Context, blockID iotago.BlockID) (*iotago.Block, error) {
	inxMsg, err := n.client.ReadBlock(ctx, inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}

	return inxMsg.UnwrapBlock(n.apiProvider)
}

func (n *NodeBridge) ListenToBlocks(ctx context.Context, cancel context.CancelFunc, consumer func(block *iotago.Block)) error {
	defer cancel()

	stream, err := n.client.ListenToBlocks(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}

	if err := ListenToStream(ctx, stream.Recv, func(block *inx.Block) error {
		consumer(block.MustUnwrapBlock(n.apiProvider))
		return nil
	}); err != nil {
		n.LogErrorf("ListenToBlocks failed: %s", err.Error())
		return err
	}

	return nil
}
