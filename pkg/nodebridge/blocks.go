package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

// ActiveRootBlocks returns the active root blocks.
func (n *nodeBridge) ActiveRootBlocks(ctx context.Context) (map[iotago.BlockID]iotago.CommitmentID, error) {
	response, err := n.client.ReadActiveRootBlocks(ctx, &inx.NoParams{})
	if err != nil {
		return nil, err
	}

	return response.Unwrap()
}

// SubmitBlock submits the given block.
func (n *nodeBridge) SubmitBlock(ctx context.Context, block *iotago.Block) (iotago.BlockID, error) {
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

// BlockMetadata returns the block metadata for the given block ID.
func (n *nodeBridge) BlockMetadata(ctx context.Context, blockID iotago.BlockID) (*inx.BlockMetadata, error) {
	return n.client.ReadBlockMetadata(ctx, inx.NewBlockId(blockID))
}

// Block returns the block for the given block ID.
func (n *nodeBridge) Block(ctx context.Context, blockID iotago.BlockID) (*iotago.Block, error) {
	inxMsg, err := n.client.ReadBlock(ctx, inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}

	return inxMsg.UnwrapBlock(n.apiProvider)
}

// ListenToBlocks listens to blocks.
func (n *nodeBridge) ListenToBlocks(ctx context.Context, cancel context.CancelFunc, consumer func(block *iotago.Block)) error {
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
