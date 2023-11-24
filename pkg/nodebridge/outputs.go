package nodebridge

import (
	"context"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	iotaapi "github.com/iotaledger/iota.go/v4/api"
)

// Output returns the output with metadata for the given output ID.
func (n *nodeBridge) Output(ctx context.Context, outputID iotago.OutputID) (outputWithMetadata *iotaapi.OutputWithMetadataResponse, rawData []byte, err error) {
	inxOutputReponse, err := n.client.ReadOutput(ctx, inx.NewOutputId(outputID))
	if err != nil {
		return nil, nil, err
	}

	metadata := &iotaapi.OutputMetadata{}

	inxOutput := inxOutputReponse.GetOutput()
	if spent := inxOutputReponse.GetSpent(); spent != nil {
		inxOutput = spent.GetOutput()

		metadata.IsSpent = true
		metadata.CommitmentIDSpent = spent.GetCommitmentIdSpent().Unwrap()
		metadata.TransactionIDSpent = spent.UnwrapTransactionIDSpent()
	}

	metadata.BlockID = inxOutput.UnwrapBlockID()
	metadata.TransactionID = outputID.TransactionID()
	metadata.OutputIndex = outputID.Index()
	metadata.IncludedCommitmentID = iotago.CommitmentID{}
	if commitmentIDIncluded := inxOutput.GetCommitmentIdIncluded(); commitmentIDIncluded != nil {
		metadata.IncludedCommitmentID = commitmentIDIncluded.Unwrap()
	}
	metadata.LatestCommitmentID = inxOutputReponse.GetLatestCommitmentId().Unwrap()

	api := n.apiProvider.APIForSlot(metadata.LatestCommitmentID.Slot())

	output, err := inxOutput.UnwrapOutput(api)
	if err != nil {
		return nil, nil, err
	}

	outputIDProof, err := inxOutput.UnwrapOutputIDProof(api)
	if err != nil {
		return nil, nil, err
	}

	derivedOutputID, err := outputIDProof.OutputID(output)
	if err != nil {
		return nil, nil, err
	}

	if derivedOutputID != outputID {
		return nil, nil, ierrors.Errorf("output ID mismatch. Expected %s, got %s", outputID.ToHex(), derivedOutputID.ToHex())
	}

	rawOutputData := inxOutput.GetOutput().GetData()

	return &iotaapi.OutputWithMetadataResponse{
		Output:        output,
		OutputIDProof: outputIDProof,
		Metadata:      metadata,
	}, rawOutputData, nil
}
