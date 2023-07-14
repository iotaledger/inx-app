/*
package nodebridge


import (
	"context"
	"io"
	"sync"
	"time"

	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"

	"github.com/iotaledger/hive.go/ierrors"
	inx "github.com/iotaledger/inx/go"
)

type TipPoolListener struct {
	nodeBridge *NodeBridge
	interval   time.Duration

	tipsMetricMutex  sync.RWMutex
	nonLazyPoolSize  uint32
	semiLazyPoolSize uint32
}

func NewTipPoolListener(nodeBridge *NodeBridge, interval time.Duration) *TipPoolListener {
	return &TipPoolListener{
		nodeBridge: nodeBridge,
		interval:   interval,
	}
}

func (t *TipPoolListener) Run(ctx context.Context) {
	c, cancel := context.WithCancel(ctx)
	defer cancel()

	go func() {
		if err := t.listenToTipsMetrics(c, cancel); err != nil {
			t.nodeBridge.LogErrorf("Error listening to tip metrics: %s", err)
		}
	}()

	<-c.Done()
}

func (t *TipPoolListener) listenToTipsMetrics(ctx context.Context, cancel context.CancelFunc) error {
	defer cancel()

	stream, err := t.nodeBridge.Client().ListenToTipsMetrics(ctx, &inx.TipsMetricRequest{IntervalInMilliseconds: uint32(t.interval.Milliseconds())})
	if err != nil {
		return err
	}

	for {
		tipsMetric, err := stream.Recv()
		if err != nil {
			if ierrors.Is(err, io.EOF) || status.Code(err) == codes.Canceled {
				break
			}
			t.nodeBridge.LogErrorf("ListenToTipsMetrics: %s", err.Error())

			break
		}
		if ctx.Err() != nil {
			break
		}
		t.processTipsMetric(tipsMetric)
	}

	//nolint:nilerr // false positive
	return nil
}

func (t *TipPoolListener) processTipsMetric(metric *inx.TipsMetric) {
	t.tipsMetricMutex.Lock()
	defer t.tipsMetricMutex.Unlock()
	t.nonLazyPoolSize = metric.GetNonLazyPoolSize()
	t.semiLazyPoolSize = metric.GetSemiLazyPoolSize()
}

func (t *TipPoolListener) GetTipsPoolSizes() (uint32, uint32) {
	t.tipsMetricMutex.RLock()
	defer t.tipsMetricMutex.RUnlock()

	return t.nonLazyPoolSize, t.semiLazyPoolSize
}
*/