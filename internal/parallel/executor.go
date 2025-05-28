package parallel

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// Task represents a unit of work to be executed
type Task struct {
	ID       string
	Name     string
	Execute  func(ctx context.Context) (interface{}, error)
	Priority int // Higher priority tasks execute first
}

// Result represents the result of a task execution
type Result struct {
	TaskID   string
	TaskName string
	Value    interface{}
	Error    error
	Duration time.Duration
}

// Executor manages parallel execution of tasks
type Executor struct {
	workerCount    int
	taskQueue      chan *Task
	resultQueue    chan *Result
	maxQueueSize   int
	ctx            context.Context
	cancel         context.CancelFunc
	wg             sync.WaitGroup
	tasksSubmitted int64
	tasksCompleted int64
	tasksFailed    int64
}

// NewExecutor creates a new parallel executor
func NewExecutor(workerCount, maxQueueSize int) *Executor {
	if workerCount <= 0 {
		workerCount = runtime.NumCPU()
	}
	if maxQueueSize <= 0 {
		maxQueueSize = workerCount * 10
	}

	ctx, cancel := context.WithCancel(context.Background())

	e := &Executor{
		workerCount:  workerCount,
		taskQueue:    make(chan *Task, maxQueueSize),
		resultQueue:  make(chan *Result, maxQueueSize),
		maxQueueSize: maxQueueSize,
		ctx:          ctx,
		cancel:       cancel,
	}

	// Start workers
	for i := 0; i < workerCount; i++ {
		e.wg.Add(1)
		go e.worker(i)
	}

	return e
}

// worker processes tasks from the queue
func (e *Executor) worker(id int) {
	defer e.wg.Done()

	for {
		select {
		case <-e.ctx.Done():
			return
		case task, ok := <-e.taskQueue:
			if !ok {
				return
			}

			// Execute the task
			start := time.Now()
			value, err := task.Execute(e.ctx)
			duration := time.Since(start)

			// Update counters
			atomic.AddInt64(&e.tasksCompleted, 1)
			if err != nil {
				atomic.AddInt64(&e.tasksFailed, 1)
			}

			// Send result
			result := &Result{
				TaskID:   task.ID,
				TaskName: task.Name,
				Value:    value,
				Error:    err,
				Duration: duration,
			}

			select {
			case e.resultQueue <- result:
			case <-e.ctx.Done():
				return
			}
		}
	}
}

// Submit submits a task for execution
func (e *Executor) Submit(task *Task) error {
	select {
	case <-e.ctx.Done():
		return fmt.Errorf("executor is shutting down")
	case e.taskQueue <- task:
		atomic.AddInt64(&e.tasksSubmitted, 1)
		return nil
	default:
		return fmt.Errorf("task queue is full")
	}
}

// SubmitBatch submits multiple tasks for execution
func (e *Executor) SubmitBatch(tasks []*Task) error {
	for _, task := range tasks {
		if err := e.Submit(task); err != nil {
			return fmt.Errorf("failed to submit task %s: %w", task.ID, err)
		}
	}
	return nil
}

// Results returns the result channel
func (e *Executor) Results() <-chan *Result {
	return e.resultQueue
}

// WaitForResults waits for all submitted tasks to complete and returns results
func (e *Executor) WaitForResults() []Result {
	// Close task queue to signal no more tasks
	close(e.taskQueue)

	// Wait for all workers to finish
	e.wg.Wait()

	// Close result queue
	close(e.resultQueue)

	// Collect all results
	var results []Result
	for result := range e.resultQueue {
		results = append(results, *result)
	}

	return results
}

// Shutdown gracefully shuts down the executor
func (e *Executor) Shutdown() {
	// Cancel context to stop workers
	e.cancel()
	
	// Only close taskQueue if it's not already closed
	select {
	case <-e.taskQueue:
		// Already closed
	default:
		close(e.taskQueue)
	}
	
	// Wait for workers to finish
	e.wg.Wait()
	
	// Only close resultQueue if it's not already closed
	select {
	case <-e.resultQueue:
		// Already closed
	default:
		close(e.resultQueue)
	}
}

// Stats returns execution statistics
func (e *Executor) Stats() ExecutorStats {
	return ExecutorStats{
		WorkerCount:    e.workerCount,
		TasksSubmitted: atomic.LoadInt64(&e.tasksSubmitted),
		TasksCompleted: atomic.LoadInt64(&e.tasksCompleted),
		TasksFailed:    atomic.LoadInt64(&e.tasksFailed),
		QueueSize:      len(e.taskQueue),
		ResultsWaiting: len(e.resultQueue),
	}
}

// ExecutorStats represents executor statistics
type ExecutorStats struct {
	WorkerCount    int
	TasksSubmitted int64
	TasksCompleted int64
	TasksFailed    int64
	QueueSize      int
	ResultsWaiting int
}

// BatchExecutor executes tasks in batches with rate limiting
type BatchExecutor struct {
	executor       *Executor
	batchSize      int
	batchDelay     time.Duration
	maxConcurrency int
	semaphore      chan struct{}
}

// NewBatchExecutor creates a new batch executor
func NewBatchExecutor(workerCount, batchSize, maxConcurrency int, batchDelay time.Duration) *BatchExecutor {
	return &BatchExecutor{
		executor:       NewExecutor(workerCount, batchSize*2),
		batchSize:      batchSize,
		batchDelay:     batchDelay,
		maxConcurrency: maxConcurrency,
		semaphore:      make(chan struct{}, maxConcurrency),
	}
}

// ExecuteBatch executes a batch of tasks with rate limiting
func (be *BatchExecutor) ExecuteBatch(ctx context.Context, tasks []*Task) ([]Result, error) {
	results := make([]Result, 0, len(tasks))
	var mu sync.Mutex

	// Process tasks in batches
	for i := 0; i < len(tasks); i += be.batchSize {
		batch := tasks[i:min(i+be.batchSize, len(tasks))]

		// Submit batch
		for _, task := range batch {
			// Acquire semaphore
			select {
			case be.semaphore <- struct{}{}:
			case <-ctx.Done():
				return results, ctx.Err()
			}

			// Wrap task to release semaphore
			wrappedTask := &Task{
				ID:       task.ID,
				Name:     task.Name,
				Priority: task.Priority,
				Execute: func(ctx context.Context) (interface{}, error) {
					defer func() { <-be.semaphore }()
					return task.Execute(ctx)
				},
			}

			if err := be.executor.Submit(wrappedTask); err != nil {
				return results, err
			}
		}

		// Delay between batches
		if i+be.batchSize < len(tasks) && be.batchDelay > 0 {
			time.Sleep(be.batchDelay)
		}
	}

	// Collect results
	go func() {
		for result := range be.executor.Results() {
			mu.Lock()
			results = append(results, *result)
			mu.Unlock()
		}
	}()

	// Wait for completion
	finalResults := be.executor.WaitForResults()
	return finalResults, nil
}

// Shutdown shuts down the batch executor
func (be *BatchExecutor) Shutdown() {
	be.executor.Shutdown()
}

// min returns the minimum of two integers
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// ParallelMap applies a function to all items in parallel
func ParallelMap[T any, R any](ctx context.Context, items []T, fn func(context.Context, T) (R, error), maxConcurrency int) ([]R, error) {
	if maxConcurrency <= 0 {
		maxConcurrency = runtime.NumCPU()
	}

	executor := NewExecutor(maxConcurrency, len(items))
	defer executor.Shutdown()

	// Create tasks
	tasks := make([]*Task, len(items))
	for i, item := range items {
		idx := i
		itemCopy := item
		tasks[i] = &Task{
			ID:   fmt.Sprintf("item-%d", idx),
			Name: fmt.Sprintf("Process item %d", idx),
			Execute: func(ctx context.Context) (interface{}, error) {
				return fn(ctx, itemCopy)
			},
		}
	}

	// Submit all tasks
	if err := executor.SubmitBatch(tasks); err != nil {
		return nil, err
	}

	// Wait for results
	results := executor.WaitForResults()

	// Sort results by task ID to maintain order
	orderedResults := make([]R, len(items))
	var firstError error
	for _, result := range results {
		// Extract index from task ID
		var idx int
		fmt.Sscanf(result.TaskID, "item-%d", &idx)

		if result.Error != nil && firstError == nil {
			firstError = result.Error
		}

		if result.Value != nil {
			orderedResults[idx] = result.Value.(R)
		}
	}

	return orderedResults, firstError
}