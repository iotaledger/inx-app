//nolint:nosnakecase // grpc uses underscores
package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

var (
	ErrLedgerUpdateTransactionAlreadyInProgress = ierrors.New("trying to begin a ledger update transaction with an already active transaction")
	ErrLedgerUpdateInvalidOperation             = ierrors.New("trying to process a ledger update operation without active transaction")
	ErrLedgerUpdateEndedAbruptly                = ierrors.New("ledger update transaction ended before receiving all operations")
)

type OutputWithMetadataAndRawData struct {
	OutputWithMetadata *api.OutputWithMetadataResponse
	RawOutputData      []byte
}

type LedgerUpdate struct {
	API          iotago.API
	CommitmentID iotago.CommitmentID
	Consumed     []*OutputWithMetadataAndRawData
	Created      []*OutputWithMetadataAndRawData
}

// ListenToLedgerUpdates listens to ledger updates.
func (n *nodeBridge) ListenToLedgerUpdates(ctx context.Context, startSlot, endSlot iotago.SlotIndex, consumer func(update *LedgerUpdate) error) error {
	req := &inx.SlotRangeRequest{
		StartSlot: uint32(startSlot),
		EndSlot:   uint32(endSlot),
	}

	stream, err := n.client.ListenToLedgerUpdates(ctx, req)
	if err != nil {
		return err
	}

	var update *LedgerUpdate
	var latestCommitmentID iotago.CommitmentID
	if err := ListenToStream(ctx, stream.Recv, func(payload *inx.LedgerUpdate) error {
		switch op := payload.GetOp().(type) {

		case *inx.LedgerUpdate_BatchMarker:
			switch op.BatchMarker.GetMarkerType() {

			case inx.LedgerUpdate_Marker_BEGIN:
				commitmentID := op.BatchMarker.GetCommitmentId().Unwrap()
				n.LogDebugf("BEGIN batch: commitmentID: %s, consumed: %d, created: %d", commitmentID, op.BatchMarker.GetConsumedCount(), op.BatchMarker.GetCreatedCount())
				if update != nil {
					return ErrLedgerUpdateTransactionAlreadyInProgress
				}

				update = &LedgerUpdate{
					API:          n.apiProvider.APIForSlot(commitmentID.Slot()),
					CommitmentID: commitmentID,
					Consumed:     make([]*OutputWithMetadataAndRawData, 0),
					Created:      make([]*OutputWithMetadataAndRawData, 0),
				}
				latestCommitmentID = n.LatestCommitment().CommitmentID

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

				if err := consumer(update); err != nil {
					return err
				}
				update = nil
			}

		case *inx.LedgerUpdate_Consumed:
			if update == nil {
				return ErrLedgerUpdateInvalidOperation
			}

			outputWithMetadataAndRawData, err := n.unwrapOutputWithMetadata(op.Consumed.GetOutput(), op.Consumed, latestCommitmentID)
			if err != nil {
				return ierrors.Wrap(err, "unable to unwrap consumed output")
			}

			update.Consumed = append(update.Consumed, outputWithMetadataAndRawData)

		case *inx.LedgerUpdate_Created:
			if update == nil {
				return ErrLedgerUpdateInvalidOperation
			}

			outputWithMetadataAndRawData, err := n.unwrapOutputWithMetadata(op.Created, nil, latestCommitmentID)
			if err != nil {
				return ierrors.Wrap(err, "unable to unwrap created output")
			}

			update.Created = append(update.Created, outputWithMetadataAndRawData)
		}

		return nil
	}); err != nil {
		n.LogErrorf("ListenToLedgerUpdates failed: %s", err.Error())
		return err
	}

	return nil
}

type AcceptedTransaction struct {
	API           iotago.API
	Slot          iotago.SlotIndex
	TransactionID iotago.TransactionID
	Consumed      []*OutputWithMetadataAndRawData
	Created       []*OutputWithMetadataAndRawData
}

// ListenToAcceptedTransactions listens to accepted transactions.
func (n *nodeBridge) ListenToAcceptedTransactions(ctx context.Context, consumer func(*AcceptedTransaction) error) error {
	stream, err := n.client.ListenToAcceptedTransactions(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}

	if err := ListenToStream(ctx, stream.Recv, func(tx *inx.AcceptedTransaction) error {
		slot := iotago.SlotIndex(tx.GetSlot())

		latestCommitmentID := n.LatestCommitment().CommitmentID

		inxSpents := tx.GetConsumed()
		consumed := make([]*OutputWithMetadataAndRawData, 0, len(inxSpents))
		for _, inxSpent := range inxSpents {
			outputWithMetadataAndRawData, err := n.unwrapOutputWithMetadata(inxSpent.GetOutput(), inxSpent, latestCommitmentID)
			if err != nil {
				return ierrors.Wrap(err, "unable to unwrap consumed output")
			}

			consumed = append(consumed, outputWithMetadataAndRawData)
		}

		inxOutputs := tx.GetCreated()
		created := make([]*OutputWithMetadataAndRawData, 0, len(inxOutputs))
		for _, inxOutput := range inxOutputs {
			outputWithMetadataAndRawData, err := n.unwrapOutputWithMetadata(inxOutput, nil, latestCommitmentID)
			if err != nil {
				return ierrors.Wrap(err, "unable to unwrap created output")
			}

			created = append(created, outputWithMetadataAndRawData)
		}

		return consumer(&AcceptedTransaction{
			API:           n.apiProvider.APIForSlot(slot),
			Slot:          slot,
			TransactionID: tx.TransactionId.Unwrap(),
			Consumed:      consumed,
			Created:       created,
		})
	}); err != nil {
		n.LogErrorf("ListenToAcceptedTransactions failed: %s", err.Error())
		return err
	}

	return nil
}
