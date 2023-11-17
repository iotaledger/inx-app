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
	API          iotago.API
	CommitmentID iotago.CommitmentID
	Consumed     []*inx.LedgerSpent
	Created      []*inx.LedgerOutput
}

func (n *NodeBridge) ListenToLedgerUpdates(ctx context.Context, startSlot, endSlot iotago.SlotIndex, consume func(update *LedgerUpdate) error) error {
	req := &inx.SlotRangeRequest{
		StartSlot: uint32(startSlot),
		EndSlot:   uint32(endSlot),
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
				commitmentID := op.BatchMarker.GetCommitmentId().Unwrap()
				n.LogDebugf("BEGIN batch: commitmentID: %s, consumed: %d, created: %d", commitmentID, op.BatchMarker.GetConsumedCount(), op.BatchMarker.GetCreatedCount())
				if update != nil {
					return ErrLedgerUpdateTransactionAlreadyInProgress
				}

				update = &LedgerUpdate{
					API:          n.apiProvider.APIForSlot(commitmentID.Slot()),
					CommitmentID: commitmentID,
					Consumed:     make([]*inx.LedgerSpent, 0),
					Created:      make([]*inx.LedgerOutput, 0),
				}

			//nolint:nosnakecase // grpc uses underscores
			case inx.LedgerUpdate_Marker_END:
				commitmentID := op.BatchMarker.GetCommitmentId().Unwrap()
				n.LogDebugf("END batch: commitmentID: %s, consumed: %d, created: %d", commitmentID, op.BatchMarker.GetConsumedCount(), op.BatchMarker.GetCreatedCount())
				if update == nil {
					return ErrLedgerUpdateInvalidOperation
				}

				if uint32(len(update.Consumed)) != op.BatchMarker.GetConsumedCount() ||
					uint32(len(update.Created)) != op.BatchMarker.GetCreatedCount() ||
					update.CommitmentID != commitmentID {
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

type AcceptedTransaction struct {
	API           iotago.API
	Slot          iotago.SlotIndex
	TransactionID iotago.TransactionID
	Consumed      []*inx.LedgerSpent
	Created       []*inx.LedgerOutput
}

func (n *NodeBridge) ListenToAcceptedTransactions(ctx context.Context, consumer func(tx *AcceptedTransaction) error) error {
	stream, err := n.client.ListenToAcceptedTransactions(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}

	for {
		tx, err := stream.Recv()
		if err != nil {
			if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			n.LogErrorf("ListenToAcceptedTransactions: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}

		slot := iotago.SlotIndex(tx.GetSlot())

		if err := consumer(&AcceptedTransaction{
			API:           n.apiProvider.APIForSlot(slot),
			Slot:          slot,
			TransactionID: tx.TransactionId.Unwrap(),
			Consumed:      tx.GetConsumed(),
			Created:       tx.GetCreated(),
		}); err != nil {
			return err
		}
	}

	//nolint:nilerr // false positive
	return nil
}
