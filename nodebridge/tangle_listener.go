package nodebridge

import (
	"context"
	"io"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/events"
	inx "github.com/iotaledger/inx/go"
	iotago "github.com/iotaledger/iota.go/v3"
)

type TangleListener struct {
	nodeBridge                  *NodeBridge
	blockSolidSyncEvent         *events.SyncEvent
	milestoneConfirmedSyncEvent *events.SyncEvent

	Events *TangleListenerEvents
}

type TangleListenerEvents struct {
	BlockSolid *events.Event
}

func INXBlockMetadataCaller(handler interface{}, params ...interface{}) {
	handler.(func(metadata *inx.BlockMetadata))(params[0].(*inx.BlockMetadata))
}

func NewTangleListener(nodeBridge *NodeBridge) *TangleListener {
	return &TangleListener{
		nodeBridge:                  nodeBridge,
		blockSolidSyncEvent:         events.NewSyncEvent(),
		milestoneConfirmedSyncEvent: events.NewSyncEvent(),
		Events: &TangleListenerEvents{
			BlockSolid: events.NewEvent(INXBlockMetadataCaller),
		},
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
		t.blockSolidSyncEvent.Trigger(metadata.GetBlockId().Unwrap())
		t.Events.BlockSolid.Trigger(metadata)
	}
	return nil
}
