package nodebridge

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	inx "github.com/iotaledger/inx/go"
)

func (n *NodeBridge) ListenToLedgerUpdates(ctx context.Context, startIndex uint32, endIndex uint32, consume func(update *inx.LedgerUpdate) error) error {
	req := &inx.MilestoneRangeRequest{
		StartMilestoneIndex: startIndex,
		EndMilestoneIndex:   endIndex,
	}

	stream, err := n.client.ListenToLedgerUpdates(ctx, req)
	if err != nil {
		return err
	}
	for {
		update, err := stream.Recv()
		if err == io.EOF || status.Code(err) == codes.Canceled {
			break
		}
		if ctx.Err() != nil {
			// context got cancelled, so stop the updates
			return nil
		}
		if err != nil {
			return err
		}
		if err := consume(update); err != nil {
			return err
		}
	}
	return nil
}
