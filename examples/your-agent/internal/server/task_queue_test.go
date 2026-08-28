package server

import (
	"context"
	"errors"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestTaskQueueRunsSessionsSeriallyAndDifferentSessionsConcurrently(t *testing.T) {
	started := make(chan string, 3)
	release := make(chan struct{}, 3)
	queue := newTaskQueue(2, 8, 4, func(item *queuedTask) {
		started <- item.task.id
		<-release
		item.task.finish(true, item.task.id, nil)
	})
	defer queue.Close()

	first := queueTestItem("task-a1", "session-a")
	second := queueTestItem("task-a2", "session-a")
	third := queueTestItem("task-b1", "session-b")
	for _, item := range []*queuedTask{first, second, third} {
		if err := queue.Enqueue(item); err != nil {
			t.Fatal(err)
		}
	}

	firstWave := map[string]bool{waitStarted(t, started): true, waitStarted(t, started): true}
	if firstWave["task-a2"] || !firstWave["task-a1"] || !firstWave["task-b1"] {
		t.Fatalf("first wave = %#v", firstWave)
	}
	release <- struct{}{}
	release <- struct{}{}
	if id := waitStarted(t, started); id != "task-a2" {
		t.Fatalf("second session-a task started as %q", id)
	}
	release <- struct{}{}
	waitDone(t, first.task.done)
	waitDone(t, second.task.done)
	waitDone(t, third.task.done)
}

func TestTaskQueueEnforcesPerSessionLimitAndCancelsPending(t *testing.T) {
	started := make(chan string, 1)
	release := make(chan struct{})
	queue := newTaskQueue(1, 8, 2, func(item *queuedTask) {
		started <- item.task.id
		<-release
		item.task.finish(true, "done", nil)
	})
	defer queue.Close()
	first := queueTestItem("task-1", "session")
	second := queueTestItem("task-2", "session")
	third := queueTestItem("task-3", "session")
	if err := queue.Enqueue(first); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started)
	if err := queue.Enqueue(second); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(third); !errors.Is(err, errSessionQueueFull) {
		t.Fatalf("enqueue error = %v", err)
	}
	if !queue.Cancel(second.task.id) {
		t.Fatal("pending task was not canceled")
	}
	waitDone(t, second.task.done)
	if status := second.task.snapshot(); status.Status != "canceled" || !status.Complete {
		t.Fatalf("status = %#v", status)
	}
	close(release)
	waitDone(t, first.task.done)
}

func TestTaskQueueEnforcesGlobalPendingLimit(t *testing.T) {
	started := make(chan string, 2)
	release := make(chan struct{})
	queue := newTaskQueue(1, 1, 4, func(item *queuedTask) {
		started <- item.task.id
		<-release
		item.task.finish(true, "done", nil)
	})
	defer queue.Close()

	first := queueTestItem("task-active", "session-a")
	second := queueTestItem("task-pending", "session-b")
	third := queueTestItem("task-rejected", "session-c")
	if err := queue.Enqueue(first); err != nil {
		t.Fatal(err)
	}
	waitStarted(t, started)
	if err := queue.Enqueue(second); err != nil {
		t.Fatal(err)
	}
	if err := queue.Enqueue(third); !errors.Is(err, errTaskQueueFull) {
		t.Fatalf("enqueue error = %v", err)
	}

	close(release)
	if id := waitStarted(t, started); id != second.task.id {
		t.Fatalf("next task started as %q", id)
	}
	waitDone(t, first.task.done)
	waitDone(t, second.task.done)
}

func TestSSECursorAndEventID(t *testing.T) {
	request := httptest.NewRequest("GET", "/api/agent/events?after=3", nil)
	request.Header.Set("Last-Event-ID", "2")
	if cursor := streamCursor(request); cursor != 3 {
		t.Fatalf("cursor = %d", cursor)
	}
	var output strings.Builder
	if !writeSSE(&output, 4, "message", map[string]any{"message": "next"}) {
		t.Fatal("writeSSE failed")
	}
	if got := output.String(); !strings.Contains(got, "id: 4\n") || !strings.Contains(got, "event: message\n") {
		t.Fatalf("SSE = %q", got)
	}
}

func queueTestItem(id, sessionID string) *queuedTask {
	ctx, cancel := context.WithCancel(context.Background())
	return &queuedTask{ctx: ctx, task: &task{
		id: id, sessionID: sessionID, status: "queued", cancel: cancel,
		approval: make(chan approvalDecision, 1), done: make(chan struct{}), createdAt: time.Now().UTC(), updatedAt: time.Now().UTC(),
	}}
}

func waitStarted(t *testing.T, started <-chan string) string {
	t.Helper()
	select {
	case id := <-started:
		return id
	case <-time.After(time.Second):
		t.Fatal("task did not start")
		return ""
	}
}

func waitDone(t *testing.T, done <-chan struct{}) {
	t.Helper()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("task did not finish")
	}
}
