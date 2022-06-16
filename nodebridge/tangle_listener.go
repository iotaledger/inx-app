package nodebridge

import (
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/events"
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
func (t *TangleListener) RegisterBlockSolidCallback(blockID iotago.BlockID, f BlockSolidCallback) error {
	if err := t.registerBlockSolidCallback(blockID, f); err != nil {
		return err
	}

	metadata, err := t.nodeBridge.BlockMetadata(blockID)
	if err == nil && metadata.Solid {
		t.triggerBlockSolidCallback(metadata)
	}
	return nil
}

func (t *TangleListener) registerBlockSolidCallback(blockID iotago.BlockID, f BlockSolidCallback) error {
	t.blockSolidCallbacksLock.Lock()
	defer t.blockSolidCallbacksLock.Unlock()

	if _, ok := t.blockSolidCallbacks[blockID]; ok {
		return fmt.Errorf("%w: block %s", ErrAlreadyRegistered, blockID.ToHex())
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

func (t *TangleListener) RegisterBlockSolidEvent(blockID iotago.BlockID) chan struct{} {

	blockSolidChan := t.blockSolidSyncEvent.RegisterEvent(blockID)

	// check if the block is already solid
	metadata, err := t.nodeBridge.BlockMetadata(blockID)
	if err == nil {
		if metadata.Solid {
			// trigger the sync event, because the block is already solid
			t.blockSolidSyncEvent.Trigger(metadata.UnwrapBlockID())
		}
	}

	return blockSolidChan
}

func (t *TangleListener) DeregisterBlockSolidEvent(blockID iotago.BlockID) {
	t.blockSolidSyncEvent.DeregisterEvent(blockID)
}

func (t *TangleListener) RegisterMilestoneConfirmedEvent(msIndex uint32) chan struct{} {
	return t.milestoneConfirmedSyncEvent.RegisterEvent(msIndex)
}

func (t *TangleListener) DeregisterMilestoneConfirmedEvent(msIndex uint32) {
	t.milestoneConfirmedSyncEvent.DeregisterEvent(msIndex)
}

func (t *TangleListener) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()
	go t.listenToSolidBlocks(c, cancel)

	onMilestoneConfirmed := events.NewClosure(func(ms *Milestone) {
		t.milestoneConfirmedSyncEvent.Trigger(ms.Milestone.Index)
	})

	t.nodeBridge.Events.ConfirmedMilestoneChanged.Attach(onMilestoneConfirmed)
	<-c.Done()
	t.nodeBridge.Events.ConfirmedMilestoneChanged.Detach(onMilestoneConfirmed)
}

func (t *TangleListener) listenToSolidBlocks(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()
	stream, err := t.nodeBridge.Client().ListenToSolidBlocks(ctx, &inx.NoParams{})
	if err != nil {
		return err
	}
	for {
		metadata, err := stream.Recv()
		if err != nil {
			if err == io.EOF || status.Code(err) == codes.Canceled {
				break
			}
			t.nodeBridge.LogErrorf("listenToSolidBlocks: %s", err.Error())
			break
		}
		if ctx.Err() != nil {
			break
		}
		t.triggerBlockSolidCallback(metadata)
		t.blockSolidSyncEvent.Trigger(metadata.GetBlockId().Unwrap())
		t.Events.BlockSolid.Trigger(metadata)
	}
	return nil
}
