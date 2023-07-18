package nodebridge

/*

import (
	"context"
	"io"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/ierrors"
	"github.com/iotaledger/hive.go/runtime/event"
	"github.com/iotaledger/hive.go/runtime/valuenotifier"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v4"
)

// ErrAlreadyRegistered is returned when a callback for the same block ID has already been registered.
var ErrAlreadyRegistered = ierrors.New("callback for block ID is already registered")

type TangleListener struct {
	nodeBridge                  *NodeBridge
	blockSolidNotifier          *valuenotifier.Notifier[iotago.BlockID]
	commitmentConfirmedNotifier *valuenotifier.Notifier[uint32]

	blockSolidCallbacks     map[iotago.BlockID]BlockSolidCallback
	blockSolidCallbacksLock sync.Mutex

	Events *TangleListenerEvents
}

type TangleListenerEvents struct {
	BlockSolid *event.Event1[*inx.BlockMetadata]
}

type BlockSolidCallback = func(*inx.BlockMetadata)

func NewTangleListener(nodeBridge *NodeBridge) *TangleListener {
	return &TangleListener{
		nodeBridge:                  nodeBridge,
		blockSolidNotifier:          valuenotifier.New[iotago.BlockID](),
		commitmentConfirmedNotifier: valuenotifier.New[uint32](),
		blockSolidCallbacks:         map[iotago.BlockID]BlockSolidCallback{},
		Events: &TangleListenerEvents{
			BlockSolid: event.New1[*inx.BlockMetadata](),
		},
	}
}

// RegisterBlockSolidCallback registers a callback for when a block with blockID becomes solid.
// If another callback for the same ID has already been registered, an error is returned.
func (t *TangleListener) RegisterBlockSolidCallback(ctx context.Context, blockID iotago.BlockID, f BlockSolidCallback) error {
	if err := t.registerBlockSolidCallback(blockID, f); err != nil {
		return err
	}

	metadata, err := t.nodeBridge.BlockMetadata(ctx, blockID)
	if err != nil {
		// if the block is not found, then it is also not yet solid
		if status.Code(err) == codes.NotFound {
			return nil
		}

		return err
	}
	if metadata.Solid {
		// trigger the callback, because the block is already solid
		t.triggerBlockSolidCallback(metadata)
	}

	return nil
}

func (t *TangleListener) registerBlockSolidCallback(blockID iotago.BlockID, f BlockSolidCallback) error {
	t.blockSolidCallbacksLock.Lock()
	defer t.blockSolidCallbacksLock.Unlock()

	if _, ok := t.blockSolidCallbacks[blockID]; ok {
		return ierrors.Wrapf(ErrAlreadyRegistered, "block %s", blockID)
	}
	t.blockSolidCallbacks[blockID] = f

	return nil
}

// DeregisterBlockSolidCallback removes a previously registered callback for blockID.
func (t *TangleListener) DeregisterBlockSolidCallback(blockID iotago.BlockID) {
	t.blockSolidCallbacksLock.Lock()
	defer t.blockSolidCallbacksLock.Unlock()
	delete(t.blockSolidCallbacks, blockID)
}

// ClearBlockSolidCallbacks removes all previously registered blockSolidCallbacks.
func (t *TangleListener) ClearBlockSolidCallbacks() {
	t.blockSolidCallbacksLock.Lock()
	defer t.blockSolidCallbacksLock.Unlock()
	t.blockSolidCallbacks = map[iotago.BlockID]BlockSolidCallback{}
}

func (t *TangleListener) triggerBlockSolidCallback(metadata *inx.BlockMetadata) {
	id := metadata.GetBlockId().Unwrap()

	t.blockSolidCallbacksLock.Lock()
	defer t.blockSolidCallbacksLock.Unlock()
	if f, ok := t.blockSolidCallbacks[id]; ok {
		go f(metadata)
		delete(t.blockSolidCallbacks, id)
	}
}

// RegisterBlockSolidEvent registers an event for when the block with blockID becomes solid.
// If the block is already solid, the event is triggered imitatively.
func (t *TangleListener) RegisterBlockSolidEvent(ctx context.Context, blockID iotago.BlockID) (*valuenotifier.Listener, error) {
	blockSolidListener := t.blockSolidNotifier.Listener(blockID)

	// check if the block is already solid
	metadata, err := t.nodeBridge.BlockMetadata(ctx, blockID)
	if err != nil {
		// if the block is not found, then it is also not yet solid
		if status.Code(err) == codes.NotFound {
			return blockSolidListener, nil
		}

		return nil, err
	}
	if metadata.Solid {
		// trigger the sync event, because the block is already solid
		t.blockSolidNotifier.Notify(metadata.UnwrapBlockID())
	}

	return blockSolidListener, nil
}

// RegisterSlotConfirmedEvent registers an event for when the slot with sIndex gets confirmed.
// If the slot is already confirmed, the event is triggered imitatively.
func (t *TangleListener) RegisterSlotConfirmedEvent(sIndex uint32) *valuenotifier.Listener {
	slotConfirmedListener := t.commitmentConfirmedNotifier.Listener(sIndex)

	// check if the slot is already confirmed
	comm, err := t.nodeBridge.ConfirmedCommitment()
	if err != nil {
		// this should never fail
		panic(err)
	}
	if comm != nil && comm.Commitment.Index >= iotago.SlotIndex(sIndex) {
		// trigger the sync event, because the slot is already confirmed
		t.commitmentConfirmedNotifier.Notify(sIndex)
	}

	return slotConfirmedListener
}

func (t *TangleListener) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := t.listenToSolidBlocks(c, cancel); err != nil {
			t.nodeBridge.LogErrorf("Error listening to solid blocks: %s", err)
		}
	}()

	hook := t.nodeBridge.Events.LatestFinalizedSlotChanged.Hook(func(c *Commitment) {
		t.commitmentConfirmedNotifier.Notify(uint32(c.Commitment.Index))
	})
	defer hook.Unhook()
	<-c.Done()
}

func (t *TangleListener) listenToSolidBlocks(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	stream, err := t.nodeBridge.Client().ListenToSolidBlocks(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}

	// receive until the context is canceled
	for ctx.Err() == nil {
		metadata, err := stream.Recv()
		if err != nil {
			if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}

			return err
		}

		t.triggerBlockSolidCallback(metadata)
		t.blockSolidNotifier.Notify(metadata.GetBlockId().Unwrap())
		t.Events.BlockSolid.Trigger(metadata)
	}

	return nil
}
*/
