// Package queue provides a per-database serialised write queue backed by
// Go channels. Each database gets its own goroutine; writes are submitted
// as closures and the result is returned via a per-call reply channel.
package queue

import (
	"context"
	"fmt"
	"sync"

	"github.com/rs/zerolog/log"
)

// WriteFunc is the closure submitted to the queue. It receives no arguments —
// the caller captures whatever it needs via closure. It must return an error.
type WriteFunc func() error

type job struct {
	fn    WriteFunc
	reply chan<- error
}

// Queue manages per-database write goroutines.
type Queue struct {
	mu       sync.Mutex
	wg       sync.WaitGroup
	workers  map[string]chan job
	maxDepth int
}

// New creates a Queue where each per-db channel has capacity maxDepth.
// maxDepth must be positive; a zero or negative value is replaced with 256.
func New(maxDepth int) *Queue {
	if maxDepth <= 0 {
		maxDepth = 256
	}
	return &Queue{
		workers:  make(map[string]chan job),
		maxDepth: maxDepth,
	}
}

// Enqueue submits fn to the serialised write goroutine for dbName and blocks
// until the write completes or ctx is cancelled.
func (q *Queue) Enqueue(ctx context.Context, dbName string, fn WriteFunc) error {
	ch := q.workerFor(dbName)
	reply := make(chan error, 1)
	select {
	case ch <- job{fn: fn, reply: reply}:
	case <-ctx.Done():
		return fmt.Errorf("queue: enqueue cancelled for %s: %w", dbName, ctx.Err())
	}
	select {
	case err := <-reply:
		return err
	case <-ctx.Done():
		return fmt.Errorf("queue: wait cancelled for %s: %w", dbName, ctx.Err())
	}
}

func (q *Queue) workerFor(dbName string) chan job {
	q.mu.Lock()
	defer q.mu.Unlock()
	if ch, ok := q.workers[dbName]; ok {
		return ch
	}
	ch := make(chan job, q.maxDepth)
	q.workers[dbName] = ch
	q.wg.Add(1)
	go func() {
		defer q.wg.Done()
		runWorker(dbName, ch)
	}()
	return ch
}

func runWorker(dbName string, ch <-chan job) {
	log.Debug().Str("db", dbName).Msg("write queue worker started")
	for j := range ch {
		err := j.fn()
		j.reply <- err
	}
	log.Debug().Str("db", dbName).Msg("write queue worker stopped")
}

// Stop drains and closes all worker channels, then waits for all goroutines to
// finish processing in-flight jobs. Call during graceful shutdown.
func (q *Queue) Stop() {
	q.mu.Lock()
	for name, ch := range q.workers {
		close(ch)
		delete(q.workers, name)
		log.Debug().Str("db", name).Msg("write queue worker closed")
	}
	q.mu.Unlock()
	// Wait for all workers to drain their channels before returning.
	q.wg.Wait()
}

// Depth returns the total number of pending jobs across all per-db channels.
func (q *Queue) Depth() int {
	q.mu.Lock()
	defer q.mu.Unlock()
	var n int
	for _, ch := range q.workers {
		n += len(ch)
	}
	return n
}
