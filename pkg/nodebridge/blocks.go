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

func (n *NodeBridge) SubmitBlock(ctx context.Context, block *iotago.ProtocolBlock) (iotago.BlockID, error) {
	apiForVersion, err := n.apiProvider.APIForVersion(block.ProtocolVersion)
	if err != nil {
		return iotago.BlockID{}, err
	}

	blk, err := inx.WrapBlock(block, apiForVersion)
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

func (n *NodeBridge) Block(ctx context.Context, blockID iotago.BlockID) (*iotago.ProtocolBlock, error) {
	inxMsg, err := n.client.ReadBlock(ctx, inx.NewBlockId(blockID))
	if err != nil {
		return nil, err
	}

	return inxMsg.UnwrapBlock(n.apiProvider.APIForSlot(blockID.Index()))
}

func (n *NodeBridge) ListenToBlocks(ctx context.Context, cancel context.CancelFunc, consumer func(block *iotago.ProtocolBlock)) error {
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

		consumer(block.MustUnwrapBlock(n.apiProvider.APIForSlot(block.UnwrapBlockID().Index())))
	}

	//nolint:nilerr // false positive
	return nil
}
