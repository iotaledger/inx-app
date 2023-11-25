package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	iotaapi "github.com/iotaledger/iota.go/v4/api"
)

func (n *nodeBridge) unwrapOutputWithMetadata(inxOutput *inx.LedgerOutput, inxSpent *inx.LedgerSpent, latestCommitmentID iotago.CommitmentID) (*OutputWithMetadataAndRawData, error) {
	outputID := inxOutput.UnwrapOutputID()

	metadata := &iotaapi.OutputMetadata{
		BlockID:              inxOutput.UnwrapBlockID(),
		TransactionID:        outputID.TransactionID(),
		OutputIndex:          outputID.Index(),
		IncludedCommitmentID: iotago.CommitmentID{},
		LatestCommitmentID:   latestCommitmentID,
	}

	if commitmentIDIncluded := inxOutput.GetCommitmentIdIncluded(); commitmentIDIncluded != nil {
		metadata.IncludedCommitmentID = commitmentIDIncluded.Unwrap()
	}

	if inxSpent != nil {
		metadata.IsSpent = true
		metadata.CommitmentIDSpent = inxSpent.GetCommitmentIdSpent().Unwrap()
		metadata.TransactionIDSpent = inxSpent.UnwrapTransactionIDSpent()
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

	return &OutputWithMetadataAndRawData{
		OutputWithMetadata: &iotaapi.OutputWithMetadataResponse{
			Output:        output,
			OutputIDProof: outputIDProof,
			Metadata:      metadata,
		},
		RawOutputData: inxOutput.GetOutput().GetData(),
	}, nil
}

// Output returns the output with metadata for the given output ID.
func (n *nodeBridge) Output(ctx context.Context, outputID iotago.OutputID) (*OutputWithMetadataAndRawData, error) {
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

	return n.unwrapOutputWithMetadata(inxOutput, inxSpent, inxOutputReponse.GetLatestCommitmentId().Unwrap())
}
