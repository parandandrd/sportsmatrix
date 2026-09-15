package sync

import (
	"context"
	"sync"
)

// Task represents a unit of work to be executed by a worker.
type Task func(ctx context.Context) error

// WorkerPool manages a fixed pool of workers that process tasks concurrently.
// This reduces goroutine churn on resource-constrained systems like Raspberry Pi.
type WorkerPool struct {
	workers   int
	taskChan  chan Task
	wg        sync.WaitGroup
	ctx       context.Context
	cancel    context.CancelFunc
	closed    bool
	closeLock sync.Mutex
}

// NewWorkerPool creates a new worker pool with the specified number of workers.
func NewWorkerPool(ctx context.Context, numWorkers int) *WorkerPool {
	if numWorkers < 1 {
		numWorkers = 1
	}

	poolCtx, cancel := context.WithCancel(ctx)
	wp := &WorkerPool{
		workers:  numWorkers,
		taskChan: make(chan Task, numWorkers*2),
		ctx:      poolCtx,
		cancel:   cancel,
	}

	for i := 0; i < numWorkers; i++ {
		wp.wg.Add(1)
		go wp.worker()
	}

	return wp
}

func (wp *WorkerPool) worker() {
	defer wp.wg.Done()
	for {
		select {
		case task, ok := <-wp.taskChan:
			if !ok {
				return
			}
			if task != nil {
				_ = task(wp.ctx)
			}
		case <-wp.ctx.Done():
			return
		}
	}
}

// Submit queues a task for execution.
func (wp *WorkerPool) Submit(task Task) error {
	wp.closeLock.Lock()
	if wp.closed {
		wp.closeLock.Unlock()
		return ErrPoolClosed
	}
	wp.closeLock.Unlock()

	select {
	case wp.taskChan <- task:
		return nil
	case <-wp.ctx.Done():
		return ErrPoolClosed
	}
}

// Close gracefully shuts down the pool and waits for all workers to finish.
func (wp *WorkerPool) Close() error {
	wp.closeLock.Lock()
	if wp.closed {
		wp.closeLock.Unlock()
		return nil
	}
	wp.closed = true
	wp.closeLock.Unlock()

	close(wp.taskChan)
	wp.wg.Wait()
	wp.cancel()
	return nil
}

type PoolError string

const ErrPoolClosed PoolError = "worker pool is closed"

func (e PoolError) Error() string {
	return string(e)
}
