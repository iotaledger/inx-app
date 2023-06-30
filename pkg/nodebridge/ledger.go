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

var (
	ErrLedgerUpdateTransactionAlreadyInProgress = ierrors.New("trying to begin a ledger update transaction with an already active transaction")
	ErrLedgerUpdateInvalidOperation             = ierrors.New("trying to process a ledger update operation without active transaction")
	ErrLedgerUpdateEndedAbruptly                = ierrors.New("ledger update transaction ended before receiving all operations")
)

type LedgerUpdate struct {
	SlotIndex iotago.SlotIndex
	Consumed  []*inx.LedgerSpent
	Created   []*inx.LedgerOutput
}

func (n *NodeBridge) ListenToLedgerUpdates(ctx context.Context, startIndex uint32, endIndex uint32, consume func(update *LedgerUpdate) error) error {
	req := &inx.CommitmentRangeRequest{
		StartSlotIndex: startIndex,
		EndSlotIndex:   endIndex,
	}

	stream, err := n.client.ListenToLedgerUpdates(ctx, req)
	if err != nil {
		return err
	}

	var update *LedgerUpdate
	for {
		payload, err := stream.Recv()
		if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
			break
		}
		if ctx.Err() != nil {
			// context got canceled, so stop the updates
			//nolint:nilerr // false positive
			return nil
		}
		if err != nil {
			return err
		}

		switch op := payload.GetOp().(type) {
		//nolint:nosnakecase // grpc uses underscores
		case *inx.LedgerUpdate_BatchMarker:
			switch op.BatchMarker.GetMarkerType() {

			//nolint:nosnakecase // grpc uses underscores
			case inx.LedgerUpdate_Marker_BEGIN:
				n.LogDebugf("BEGIN batch: %d consumed: %d, created: %d", op.BatchMarker.GetMilestoneIndex(), op.BatchMarker.GetConsumedCount(), op.BatchMarker.GetCreatedCount())
				if update != nil {
					return ErrLedgerUpdateTransactionAlreadyInProgress
				}
				update = &LedgerUpdate{
					SlotIndex: iotago.SlotIndex(op.BatchMarker.GetMilestoneIndex()),
					Consumed:  make([]*inx.LedgerSpent, 0),
					Created:   make([]*inx.LedgerOutput, 0),
				}

			//nolint:nosnakecase // grpc uses underscores
			case inx.LedgerUpdate_Marker_END:
				n.LogDebugf("END batch: %d consumed: %d, created: %d", op.BatchMarker.GetMilestoneIndex(), op.BatchMarker.GetConsumedCount(), op.BatchMarker.GetCreatedCount())
				if update == nil {
					return ErrLedgerUpdateInvalidOperation
				}
				if uint32(len(update.Consumed)) != op.BatchMarker.GetConsumedCount() ||
					uint32(len(update.Created)) != op.BatchMarker.GetCreatedCount() ||
					update.SlotIndex != iotago.SlotIndex(op.BatchMarker.MilestoneIndex) {
					return ErrLedgerUpdateEndedAbruptly
				}

				if err := consume(update); err != nil {
					return err
				}
				update = nil
			}

		//nolint:nosnakecase // grpc uses underscores
		case *inx.LedgerUpdate_Consumed:
			if update == nil {
				return ErrLedgerUpdateInvalidOperation
			}
			update.Consumed = append(update.Consumed, op.Consumed)

		//nolint:nosnakecase // grpc uses underscores
		case *inx.LedgerUpdate_Created:
			if update == nil {
				return ErrLedgerUpdateInvalidOperation
			}
			update.Created = append(update.Created, op.Created)
		}
	}

	return nil
}
