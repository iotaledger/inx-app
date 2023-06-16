package pow

import (
	"context"
	"fmt"
	"time"

	"github.com/pkg/errors"

	"github.com/iotaledger/hive.go/runtime/contextutils"
	"github.com/iotaledger/hive.go/serializer/v2"
	iotago "github.com/iotaledger/iota.go/v4"
	"github.com/iotaledger/iota.go/v4/pow"
)

const (
	nonceBytes = 8 // len(uint64)
)

var (
	// ErrOperationAborted is returned when the operation was aborted e.g. by a shutdown signal.
	ErrOperationAborted = errors.New("operation was aborted")
	// ErrParentsNotGiven is returned when no block parents and no refreshTipsFunc were given.
	ErrParentsNotGiven = errors.New("no parents given")
)

// RefreshTipsFunc refreshes tips of the block if PoW takes longer than a configured duration.
type RefreshTipsFunc = func() (tips iotago.BlockIDs, err error)

// DoPoW does the proof-of-work required to hit the given target score.
// The given iota.Block's nonce is automatically updated.
func DoPoW(ctx context.Context, block *iotago.Block, _ serializer.DeSerializationMode, protoParams *iotago.ProtocolParameters, parallelism int, refreshTipsInterval time.Duration, refreshTipsFunc RefreshTipsFunc) (blockSize int, err error) {
	api := iotago.V3API(protoParams)

	if len(block.StrongParents) == 0 {
		if refreshTipsFunc == nil {
			return 0, ErrParentsNotGiven
		}

		// select initial parents
		tips, err := refreshTipsFunc()
		if err != nil {
			return 0, err
		}
		block.StrongParents = tips
	}

	if protoParams.MinPoWScore == 0 {
		block.Nonce = 0

		return 0, nil
	}

	if err := contextutils.ReturnErrIfCtxDone(ctx, ErrOperationAborted); err != nil {
		return 0, err
	}

	getPoWData := func(block *iotago.Block) (powData []byte, err error) {
		blockData, err := api.Encode(block)
		if err != nil {
			return nil, fmt.Errorf("unable to perform PoW as block can't be serialized: %w", err)
		}

		return blockData[:len(blockData)-nonceBytes], nil
	}

	powData, err := getPoWData(block)
	if err != nil {
		return 0, err
	}

	doPow := func(ctx context.Context) (uint64, error) {
		powCtx, powCancel := context.WithCancel(ctx)
		defer powCancel()

		if refreshTipsFunc != nil {
			var powTimeoutCancel context.CancelFunc
			powCtx, powTimeoutCancel = context.WithTimeout(powCtx, refreshTipsInterval)
			defer powTimeoutCancel()
		}

		nonce, err := pow.New(parallelism).Mine(powCtx, powData, float64(protoParams.MinPoWScore))
		if err != nil {
			if errors.Is(err, pow.ErrCancelled) && refreshTipsFunc != nil {
				// context was canceled and tips can be refreshed
				tips, err := refreshTipsFunc()
				if err != nil {
					return 0, err
				}
				block.StrongParents = tips

				// replace the powData to update the new tips
				powData, err = getPoWData(block)
				if err != nil {
					return 0, err
				}

				return 0, pow.ErrCancelled
			}

			return 0, err
		}

		return nonce, nil
	}

	for {
		nonce, err := doPow(ctx)
		if err != nil {
			// check if the external context got canceled.
			if ctx.Err() != nil {
				return 0, ErrOperationAborted
			}

			if errors.Is(err, pow.ErrCancelled) {
				// redo the PoW with new tips
				continue
			}

			return 0, err
		}

		block.Nonce = nonce

		return len(powData) + nonceBytes, nil
	}
}
