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

	for {
		block, err := stream.Recv()
		if err != nil {
			if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("ListenToBlocks: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}

		consumer(block.MustUnwrapBlock(n.apiProvider))
	}

	//nolint:nilerr // false positive
	return nil
}
