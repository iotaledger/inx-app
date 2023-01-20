package nodebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/core/events"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

// ErrAlreadyRegistered is returned when a callback for the same block ID has already been registered.
var ErrAlreadyRegistered = errors.New("callback for block ID is already registered")

type TangleListener struct {
	nodeBridge                  *NodeBridge
	blockSolidSyncEvent         *events.SyncEvent
	milestoneConfirmedSyncEvent *events.SyncEvent

	blockSolidCallbacks     map[iotago.BlockID]BlockSolidCallback
	blockSolidCallbacksLock sync.Mutex

	Events *TangleListenerEvents
}

type TangleListenerEvents struct {
	BlockSolid *events.Event
}

type BlockSolidCallback = func(*inx.BlockMetadata)

func INXBlockMetadataCaller(handler interface{}, params ...interface{}) {
	//nolint:forcetypeassert // we will replace that with generic events anyway
	handler.(func(metadata *inx.BlockMetadata))(params[0].(*inx.BlockMetadata))
}

func NewTangleListener(nodeBridge *NodeBridge) *TangleListener {
	return &TangleListener{
		nodeBridge:                  nodeBridge,
		blockSolidSyncEvent:         events.NewSyncEvent(),
		milestoneConfirmedSyncEvent: events.NewSyncEvent(),
		blockSolidCallbacks:         map[iotago.BlockID]BlockSolidCallback{},
		Events: &TangleListenerEvents{
			BlockSolid: events.NewEvent(INXBlockMetadataCaller),
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
		return fmt.Errorf("%w: block %s", ErrAlreadyRegistered, blockID)
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
func (t *TangleListener) RegisterBlockSolidEvent(ctx context.Context, blockID iotago.BlockID) (chan struct{}, error) {
	blockSolidChan := t.blockSolidSyncEvent.RegisterEvent(blockID)

	// check if the block is already solid
	metadata, err := t.nodeBridge.BlockMetadata(ctx, blockID)
	if err != nil {
		// if the block is not found, then it is also not yet solid
		if status.Code(err) == codes.NotFound {
			return blockSolidChan, nil
		}

		return nil, err
	}
	if metadata.Solid {
		// trigger the sync event, because the block is already solid
		t.blockSolidSyncEvent.Trigger(metadata.UnwrapBlockID())
	}

	return blockSolidChan, nil
}

// DeregisterBlockSolidEvent removes a registered solid block event by triggering it to free memory.
func (t *TangleListener) DeregisterBlockSolidEvent(blockID iotago.BlockID) {
	t.blockSolidSyncEvent.DeregisterEvent(blockID)
}

// RegisterMilestoneConfirmedEvent registers an event for when the milestone with msIndex gets confirmed.
// If the milestone is already confirmed, the event is triggered imitatively.
func (t *TangleListener) RegisterMilestoneConfirmedEvent(msIndex uint32) chan struct{} {
	milestoneConfirmedChan := t.milestoneConfirmedSyncEvent.RegisterEvent(msIndex)

	// check if the milestone is already confirmed
	ms, err := t.nodeBridge.ConfirmedMilestone()
	if err != nil {
		// this should never fail
		panic(err)
	}
	if ms != nil && ms.Milestone.Index >= msIndex {
		// trigger the sync event, because the milestone is already confirmed
		t.milestoneConfirmedSyncEvent.Trigger(msIndex)
	}

	return milestoneConfirmedChan
}

// DeregisterMilestoneConfirmedEvent removes a registered confirmed milestone event by triggering it to free memory.
func (t *TangleListener) DeregisterMilestoneConfirmedEvent(msIndex uint32) {
	t.milestoneConfirmedSyncEvent.DeregisterEvent(msIndex)
}

func (t *TangleListener) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := t.listenToSolidBlocks(c, cancel); err != nil {
			t.nodeBridge.LogErrorf("Error listening to solid blocks: %s", err)
		}
	}()

	onMilestoneConfirmed := events.NewClosure(func(ms *Milestone) {
		t.milestoneConfirmedSyncEvent.Trigger(ms.Milestone.Index)
	})

	t.nodeBridge.Events.ConfirmedMilestoneChanged.Hook(onMilestoneConfirmed)
	<-c.Done()
	t.nodeBridge.Events.ConfirmedMilestoneChanged.Detach(onMilestoneConfirmed)
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
			if errors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}

			return err
		}

		t.triggerBlockSolidCallback(metadata)
		t.blockSolidSyncEvent.Trigger(metadata.GetBlockId().Unwrap())
		t.Events.BlockSolid.Trigger(metadata)
	}

	return nil
}
