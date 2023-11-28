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

	includedCommitmentID := iotago.EmptyCommitmentID
	if commitmentIDIncluded := inxOutput.GetCommitmentIdIncluded(); commitmentIDIncluded != nil {
		includedCommitmentID = commitmentIDIncluded.Unwrap()
	}

	metadata := &iotaapi.OutputMetadata{
		OutputID: outputID,
		BlockID:  inxOutput.UnwrapBlockID(),
		Included: &iotaapi.OutputInclusionMetadata{
			Slot:          iotago.SlotIndex(inxOutput.GetSlotBooked()),
			TransactionID: outputID.TransactionID(),
			CommitmentID:  includedCommitmentID,
		},
		LatestCommitmentID: latestCommitmentID,
	}

	if inxSpent != nil {
		spentCommitmentID := iotago.EmptyCommitmentID
		if commitmentIDSpent := inxSpent.GetCommitmentIdSpent(); commitmentIDSpent != nil {
			spentCommitmentID = commitmentIDSpent.Unwrap()
		}

		metadata.Spent = &iotaapi.OutputConsumptionMetadata{
			Slot:          iotago.SlotIndex(inxSpent.GetSlotSpent()),
			TransactionID: inxSpent.UnwrapTransactionIDSpent(),
			CommitmentID:  spentCommitmentID,
		}
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
