package client

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestAcquirePIDFile_FirstCallSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.pid")

	pf, err := acquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquirePIDFile: %v", err)
	}
	defer pf.Close()

	// The file must exist and already contain *our* PID — the stamp is what
	// makes concurrent acquires see a live owner instead of a blank file.
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read pid file: %v", err)
	}
	want := fmt.Sprintf("%d", os.Getpid())
	if string(got) != want {
		t.Errorf("pid file = %q, want %q", string(got), want)
	}
}

// When a live daemon is already running, a second acquire must fail rather
// than clobber the existing PID file.
func TestAcquirePIDFile_LiveOwner_ReturnsAlreadyRunning(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.pid")

	// Seed with the current process's PID — we're definitely "running".
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", os.Getpid())), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	_, err := acquirePIDFile(path)
	if err == nil {
		t.Fatal("expected error for live PID owner")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Errorf("err = %v, want 'already running'", err)
	}
}

// A PID file left behind by a crashed daemon must be reclaimable — otherwise
// the user has to rm it by hand after every unclean shutdown.
func TestAcquirePIDFile_StalePID_ReplacesAndSucceeds(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.pid")

	// Pick a PID that's almost certainly not running. PID_MAX is typically
	// 4194304 on Linux and 99999 on macOS, so 2^30 is safely out of range.
	stale := 1 << 30
	if err := os.WriteFile(path, []byte(fmt.Sprintf("%d", stale)), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}

	pf, err := acquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquirePIDFile with stale PID: %v", err)
	}
	defer pf.Close()
}

// A malformed PID file (not a number) should be treated as stale rather than
// locking the user out forever.
func TestAcquirePIDFile_MalformedFile_TreatedAsStale(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.pid")
	if err := os.WriteFile(path, []byte("not-a-pid\n"), 0600); err != nil {
		t.Fatalf("seed: %v", err)
	}
	pf, err := acquirePIDFile(path)
	if err != nil {
		t.Fatalf("acquirePIDFile malformed: %v", err)
	}
	defer pf.Close()
}

// The whole point of O_EXCL: two concurrent acquires on the same path must
// not both succeed. Exactly one wins.
func TestAcquirePIDFile_ConcurrentAcquire_OnlyOneWins(t *testing.T) {
	path := filepath.Join(t.TempDir(), "pigeon.pid")

	var winners atomic.Int32
	var wg sync.WaitGroup
	const goroutines = 20
	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			pf, err := acquirePIDFile(path)
			if err == nil {
				winners.Add(1)
				pf.Close()
			}
		}()
	}
	wg.Wait()

	if winners.Load() != 1 {
		t.Errorf("winners = %d, want exactly 1", winners.Load())
	}
}
