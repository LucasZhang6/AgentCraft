package server

import (
	"context"
	"errors"
	"sync"
)

var (
	errTaskQueueFull    = errors.New("HTTP task queue is full")
	errSessionQueueFull = errors.New("HTTP session task queue is full")
	errTaskQueueClosed  = errors.New("HTTP task queue is stopping")
)

type queuedTask struct {
	ctx    context.Context
	task   *task
	input  ExecuteRequest
	images []string
}

type taskQueue struct {
	mu             sync.Mutex
	cond           *sync.Cond
	pending        []*queuedTask
	activeByID     map[string]*queuedTask
	activeSessions map[string]struct{}
	perSession     map[string]int
	maxPending     int
	maxPerSession  int
	stopping       bool
	run            func(*queuedTask)
	workers        sync.WaitGroup
}

func newTaskQueue(maxConcurrent, maxPending, maxPerSession int, run func(*queuedTask)) *taskQueue {
	if maxConcurrent <= 0 {
		maxConcurrent = 4
	}
	if maxPending <= 0 {
		maxPending = 256
	}
	if maxPerSession <= 0 {
		maxPerSession = 8
	}
	queue := &taskQueue{
		activeByID: make(map[string]*queuedTask), activeSessions: make(map[string]struct{}),
		perSession: make(map[string]int), maxPending: maxPending, maxPerSession: maxPerSession, run: run,
	}
	queue.cond = sync.NewCond(&queue.mu)
	for worker := 0; worker < maxConcurrent; worker++ {
		queue.workers.Add(1)
		go queue.worker()
	}
	return queue
}

func (queue *taskQueue) Enqueue(item *queuedTask) error {
	if queue == nil || item == nil || item.task == nil {
		return errors.New("invalid queued HTTP task")
	}
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.stopping {
		return errTaskQueueClosed
	}
	if len(queue.pending) >= queue.maxPending {
		return errTaskQueueFull
	}
	sessionID := item.task.sessionID
	if queue.perSession[sessionID] >= queue.maxPerSession {
		return errSessionQueueFull
	}
	queue.pending = append(queue.pending, item)
	queue.perSession[sessionID]++
	queue.cond.Broadcast()
	return nil
}

func (queue *taskQueue) Cancel(taskID string) bool {
	if queue == nil {
		return false
	}
	queue.mu.Lock()
	for index, item := range queue.pending {
		if item.task.id != taskID {
			continue
		}
		queue.pending = append(queue.pending[:index], queue.pending[index+1:]...)
		queue.decrementSessionLocked(item.task.sessionID)
		queue.cond.Broadcast()
		queue.mu.Unlock()
		item.task.cancel()
		item.task.finish(false, "", context.Canceled)
		item.task.closeDone()
		return true
	}
	active := queue.activeByID[taskID]
	queue.mu.Unlock()
	if active != nil {
		active.task.cancel()
		return true
	}
	return false
}

func (queue *taskQueue) Close() {
	if queue == nil {
		return
	}
	queue.mu.Lock()
	if queue.stopping {
		queue.mu.Unlock()
		queue.workers.Wait()
		return
	}
	queue.stopping = true
	pending := append([]*queuedTask(nil), queue.pending...)
	queue.pending = nil
	active := make([]*queuedTask, 0, len(queue.activeByID))
	for _, item := range queue.activeByID {
		active = append(active, item)
	}
	for _, item := range pending {
		queue.decrementSessionLocked(item.task.sessionID)
	}
	queue.cond.Broadcast()
	queue.mu.Unlock()
	for _, item := range pending {
		item.task.cancel()
		item.task.finish(false, "", context.Canceled)
		item.task.closeDone()
	}
	for _, item := range active {
		item.task.cancel()
	}
	queue.workers.Wait()
}

func (queue *taskQueue) worker() {
	defer queue.workers.Done()
	for {
		item := queue.next()
		if item == nil {
			return
		}
		func() {
			defer queue.complete(item)
			defer item.task.closeDone()
			if err := item.ctx.Err(); err != nil {
				item.task.finish(false, "", err)
				return
			}
			item.task.markRunning()
			queue.run(item)
		}()
	}
}

func (queue *taskQueue) next() *queuedTask {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for {
		for index, item := range queue.pending {
			if _, busy := queue.activeSessions[item.task.sessionID]; busy {
				continue
			}
			queue.pending = append(queue.pending[:index], queue.pending[index+1:]...)
			queue.activeSessions[item.task.sessionID] = struct{}{}
			queue.activeByID[item.task.id] = item
			return item
		}
		if queue.stopping {
			return nil
		}
		queue.cond.Wait()
	}
}

func (queue *taskQueue) complete(item *queuedTask) {
	queue.mu.Lock()
	delete(queue.activeByID, item.task.id)
	delete(queue.activeSessions, item.task.sessionID)
	queue.decrementSessionLocked(item.task.sessionID)
	queue.cond.Broadcast()
	queue.mu.Unlock()
}

func (queue *taskQueue) decrementSessionLocked(sessionID string) {
	if queue.perSession[sessionID] <= 1 {
		delete(queue.perSession, sessionID)
		return
	}
	queue.perSession[sessionID]--
}
