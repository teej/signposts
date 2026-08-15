package state

import (
	"sync"
	"sync/atomic"
	"testing"
)

func TestReserveIsAtomic(t *testing.T) {
	store := New(t.TempDir())
	var reserved atomic.Int32
	wait := sync.WaitGroup{}

	for range 32 {
		wait.Add(1)
		go func() {
			defer wait.Done()
			won, err := store.Reserve("/repo", "session", "rule")
			if err != nil {
				t.Errorf("reserve: %v", err)
				return
			}
			if won {
				reserved.Add(1)
			}
		}()
	}
	wait.Wait()

	if reserved.Load() != 1 {
		t.Fatalf("reserved %d times, want once", reserved.Load())
	}
	emitted, err := store.Emitted("/repo", "session")
	if err != nil {
		t.Fatal(err)
	}
	if len(emitted) != 1 || emitted[0] != "rule" {
		t.Fatalf("unexpected emitted rules: %v", emitted)
	}
}

func TestClearStartsFresh(t *testing.T) {
	store := New(t.TempDir())
	if won, err := store.Reserve("/repo", "session", "rule"); err != nil || !won {
		t.Fatalf("first reserve = %v, %v", won, err)
	}
	if err := store.Clear("/repo", "session"); err != nil {
		t.Fatal(err)
	}
	if won, err := store.Reserve("/repo", "session", "rule"); err != nil || !won {
		t.Fatalf("reserve after clear = %v, %v", won, err)
	}
}
