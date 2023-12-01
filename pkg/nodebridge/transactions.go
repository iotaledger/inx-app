package nodebridge

import (
	"context"

	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/api"
)

// TransactionMetadata returns the transaction metadata for the given transaction ID.
func (n *nodeBridge) TransactionMetadata(ctx context.Context, transactionID iotago.TransactionID) (*api.TransactionMetadataResponse, error) {
	inxTransactionMetadata, err := n.client.ReadTransactionMetadata(ctx, inx.NewTransactionId(transactionID))
	if err != nil {
		return nil, err
	}

	return inxTransactionMetadata.Unwrap(), nil
}
