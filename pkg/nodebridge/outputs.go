package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	iotaapi "github.com/iotaledger/iota.go/v4/api"
)

func (n *nodeBridge) unwrapOutput(inxOutput *inx.LedgerOutput, inxSpent *inx.LedgerSpent, latestCommitmentID iotago.CommitmentID) (*Output, error) {
	outputID := inxOutput.UnwrapOutputID()

	slotBooked := iotago.SlotIndex(inxOutput.GetSlotBooked())
	metadata := &iotaapi.OutputMetadata{
		BlockID:              inxOutput.UnwrapBlockID(),
		TransactionID:        outputID.TransactionID(),
		OutputIndex:          outputID.Index(),
		IncludedCommitmentID: iotago.EmptyCommitmentID,
		IsSpent:              false,
		CommitmentIDSpent:    iotago.EmptyCommitmentID,
		TransactionIDSpent:   iotago.EmptyTransactionID,
		LatestCommitmentID:   latestCommitmentID,
	}

	if commitmentIDIncluded := inxOutput.GetCommitmentIdIncluded(); commitmentIDIncluded != nil {
		metadata.IncludedCommitmentID = commitmentIDIncluded.Unwrap()
	}

	var slotSpent iotago.SlotIndex
	if inxSpent != nil {
		metadata.IsSpent = true
		if commitmentIDSpent := inxSpent.GetCommitmentIdSpent(); commitmentIDSpent != nil {
			metadata.CommitmentIDSpent = commitmentIDSpent.Unwrap()
		}
		metadata.TransactionIDSpent = inxSpent.UnwrapTransactionIDSpent()
		slotSpent = iotago.SlotIndex(inxSpent.GetSlotSpent())
	}

	api := n.apiProvider.APIForSlot(outputID.Slot())
	output, err := inxOutput.UnwrapOutput(api)
	if err != nil {
		return nil, err
	}

	outputIDProof, err := inxOutput.UnwrapOutputIDProof(api)
	if err != nil {
		return nil, err
	}

	derivedOutputID, err := outputIDProof.OutputID(output)
	if err != nil {
		return nil, err
	}

	if derivedOutputID != outputID {
		return nil, ierrors.Errorf("output ID mismatch. Expected %s, got %s", outputID.ToHex(), derivedOutputID.ToHex())
	}

	return &Output{
		OutputID:      outputID,
		Output:        output,
		OutputIDProof: outputIDProof,
		Metadata:      metadata,
		// we need to pass the slots here, because they can not be safely derived from "IncludedCommitmentID" and "CommitmentIDSpent"
		// on client side, because for "AcceptedTransactions" the commitments (Included, Spent) might still be empty.
		SlotBooked:    slotBooked,
		SlotSpent:     slotSpent,
		RawOutputData: inxOutput.GetOutput().GetData(),
	}, nil
}

// Output returns the output with metadata for the given output ID.
func (n *nodeBridge) Output(ctx context.Context, outputID iotago.OutputID) (*Output, error) {
	inxOutputReponse, err := n.client.ReadOutput(ctx, inx.NewOutputId(outputID))
	if err != nil {
		return nil, err
	}

	inxOutput := inxOutputReponse.GetOutput()
	inxSpent := inxOutputReponse.GetSpent()
	if inxSpent != nil {
		// if spent is not nil, the output is included in spent
		inxOutput = inxSpent.GetOutput()
	}

	return n.unwrapOutput(inxOutput, inxSpent, inxOutputReponse.GetLatestCommitmentId().Unwrap())
}
