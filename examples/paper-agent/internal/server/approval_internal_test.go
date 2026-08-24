package server

import "testing"

func TestTaskAcceptsOnlyCurrentApproval(t *testing.T) {
	current := &task{
		pendingApproval: &PendingApproval{ToolID: "tool_current"},
		approval:        make(chan approvalDecision, 1),
	}
	if current.acceptApproval("tool_old", true, true) {
		t.Fatal("accepted stale approval")
	}
	if current.remainingApproved() {
		t.Fatal("stale approval changed approve-all state")
	}
	if current.acceptApproval("tool_current", true, true) == false {
		t.Fatal("rejected current approval")
	}
	decision := <-current.approval
	if decision.toolID != "tool_current" || !decision.approved {
		t.Fatalf("decision = %#v", decision)
	}
	if !current.remainingApproved() {
		t.Fatal("approve-all did not persist for remaining tools")
	}
}
