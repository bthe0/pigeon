package client

import (
	"sync"
	"testing"

	"github.com/bthe0/pigeon/internal/proto"
)

func TestLookupForward_NilIndex_ReturnsNil(t *testing.T) {
	c := &Client{cfg: &Config{}}
	if got := c.lookupForward("missing"); got != nil {
		t.Errorf("got %v, want nil", got)
	}
}

func TestLookupForward_AfterRebuild_FindsEntries(t *testing.T) {
	c := &Client{cfg: &Config{Forwards: []ForwardRule{
		{ID: "aaa", LocalAddr: "127.0.0.1:1", Protocol: proto.ProtoHTTP},
		{ID: "bbb", LocalAddr: "127.0.0.1:2", Protocol: proto.ProtoTCP},
	}}}
	c.rebuildForwardIndex()

	if r := c.lookupForward("aaa"); r == nil || r.LocalAddr != "127.0.0.1:1" {
		t.Errorf("aaa lookup = %v", r)
	}
	if r := c.lookupForward("bbb"); r == nil || r.Protocol != proto.ProtoTCP {
		t.Errorf("bbb lookup = %v", r)
	}
	if r := c.lookupForward("ccc"); r != nil {
		t.Errorf("ccc lookup = %v, want nil", r)
	}
}

// Rebuild must swap out stale entries — a forward deleted from cfg.Forwards
// must stop being findable, not linger from the previous index.
func TestRebuildForwardIndex_ReplacesPriorEntries(t *testing.T) {
	c := &Client{cfg: &Config{Forwards: []ForwardRule{{ID: "old"}}}}
	c.rebuildForwardIndex()
	if c.lookupForward("old") == nil {
		t.Fatal("old entry not present after first build")
	}

	c.cfg.Forwards = []ForwardRule{{ID: "new"}}
	c.rebuildForwardIndex()

	if c.lookupForward("old") != nil {
		t.Error("old entry still present after rebuild")
	}
	if c.lookupForward("new") == nil {
		t.Error("new entry missing after rebuild")
	}
}

// The atomic.Pointer swap must be safe under concurrent reads — race detector
// would flag a plain map swap. Run with -race to exercise the guarantee.
func TestForwardIndex_ConcurrentReadDuringRebuild(t *testing.T) {
	c := &Client{cfg: &Config{Forwards: []ForwardRule{
		{ID: "stable"},
	}}}
	c.rebuildForwardIndex()

	var wg sync.WaitGroup
	stop := make(chan struct{})

	// Reader goroutines — hammer lookupForward.
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				select {
				case <-stop:
					return
				default:
					_ = c.lookupForward("stable")
				}
			}
		}()
	}

	// Writer goroutine — swap the index repeatedly.
	for i := 0; i < 200; i++ {
		c.cfg.Forwards = []ForwardRule{{ID: "stable"}, {ID: "extra"}}
		c.rebuildForwardIndex()
	}

	close(stop)
	wg.Wait()
}
