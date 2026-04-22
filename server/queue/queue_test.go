package queue_test

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/0xdps/sqlite-hub-template/queue"
)

// ── serial ordering ───────────────────────────────────────────────────────────

// TestOrdering verifies that writes for the same database are executed in FIFO
// order. Each write appends its index, and the result slice must be sorted.
func TestOrdering(t *testing.T) {
	q := queue.New(32)
	defer q.Stop()

	var got []int
	ctx := context.Background()
	for i := 0; i < 10; i++ {
		i := i
		if err := q.Enqueue(ctx, "db1", func() error {
			got = append(got, i)
			return nil
		}); err != nil {
			t.Fatalf("Enqueue %d: %v", i, err)
		}
	}
	if len(got) != 10 {
		t.Fatalf("len(got) = %d; want 10", len(got))
	}
	for idx, v := range got {
		if v != idx {
			t.Errorf("got[%d] = %d; want %d (not FIFO)", idx, v, idx)
		}
	}
}

// ── error propagation ─────────────────────────────────────────────────────────

func TestErrorPropagation(t *testing.T) {
	q := queue.New(8)
	defer q.Stop()

	sentinel := errors.New("write failed")
	err := q.Enqueue(context.Background(), "errdb", func() error { return sentinel })
	if !errors.Is(err, sentinel) {
		t.Errorf("Enqueue error = %v; want %v", err, sentinel)
	}
}

// ── isolation between databases ───────────────────────────────────────────────

// TestDatabaseIsolation ensures that workers for different databases are
// independent: writes on db-A do not block or interfere with db-B.
func TestDatabaseIsolation(t *testing.T) {
	q := queue.New(16)
	defer q.Stop()

	var countA, countB atomic.Int64
	ctx := context.Background()

	const n = 50
	done := make(chan struct{}, 2)

	go func() {
		for i := 0; i < n; i++ {
			_ = q.Enqueue(ctx, "alpha", func() error {
				countA.Add(1)
				return nil
			})
		}
		done <- struct{}{}
	}()
	go func() {
		for i := 0; i < n; i++ {
			_ = q.Enqueue(ctx, "beta", func() error {
				countB.Add(1)
				return nil
			})
		}
		done <- struct{}{}
	}()

	<-done
	<-done

	if countA.Load() != n {
		t.Errorf("alpha count = %d; want %d", countA.Load(), n)
	}
	if countB.Load() != n {
		t.Errorf("beta count = %d; want %d", countB.Load(), n)
	}
}

// ── context cancellation ──────────────────────────────────────────────────────

// TestContextCancellation checks that Enqueue returns when the context is
// cancelled while the job is queued but not yet picked up.
func TestContextCancellation(t *testing.T) {
	// maxDepth=0 makes the channel unbuffered — the sender blocks immediately
	// because there is no worker goroutine to drain the channel while the
	// first job holds the worker busy.
	q := queue.New(0)
	defer q.Stop()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	// The first enqueue will block waiting for a worker. With maxDepth=0 the
	// channel itself is unbuffered, so even the first send will block until
	// a worker is ready. Cancel the context immediately to test the escape path.
	cancel()
	err := q.Enqueue(ctx, "slowdb", func() error {
		time.Sleep(time.Second) // should never actually run
		return nil
	})
	if err == nil {
		t.Error("expected error from cancelled context, got nil")
	}
	if !errors.Is(err, context.Canceled) {
		t.Errorf("error = %v; want wrapping context.Canceled", err)
	}
}

// ── stop / shutdown ───────────────────────────────────────────────────────────

// TestStop ensures Stop does not panic and Clean shutdown works.
func TestStop(t *testing.T) {
	q := queue.New(4)

	ctx := context.Background()
	var count atomic.Int64
	for i := 0; i < 5; i++ {
		if err := q.Enqueue(ctx, "stopdb", func() error {
			count.Add(1)
			return nil
		}); err != nil {
			t.Fatalf("Enqueue: %v", err)
		}
	}
	if count.Load() != 5 {
		t.Errorf("count = %d; want 5 before Stop", count.Load())
	}
	// Stop should not panic.
	q.Stop()
}
