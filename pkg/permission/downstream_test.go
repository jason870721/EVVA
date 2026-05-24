package permission_test

import (
	"context"
	"testing"

	"github.com/johnny1110/evva/pkg/permission"
)

// TestDownstream_CustomPolicy is the v2.2 acceptance gate: a downstream host
// plugs in its own allow/deny policy using only pkg/permission — this file
// imports zero internal/*. The broker + SetOnRequest is the pluggability
// seam, and the fact that this compiles + passes is the proof a separate
// module can supply a real permission policy.
func TestDownstream_CustomPolicy(t *testing.T) {
	broker := permission.NewBroker()

	// A host policy: allow `read`, deny everything else. Resolving inside
	// the callback works because the broker's reply channel is buffered —
	// the same property cmd/evva's headless CLI sink depends on.
	permission.SetOnRequest(broker, func(req permission.ApprovalRequest) {
		d := permission.Decision{Behavior: permission.BehaviorDeny, Reason: "policy: default deny"}
		if req.ToolName == "read" {
			d = permission.Decision{Behavior: permission.BehaviorAllow, Reason: "policy: reads allowed"}
		}
		_ = broker.Respond(req.ID, d)
	})

	cases := []struct {
		tool string
		want permission.Behavior
	}{
		{"read", permission.BehaviorAllow},
		{"bash", permission.BehaviorDeny},
	}
	for _, tc := range cases {
		got, err := broker.Request(context.Background(), permission.ApprovalRequest{ToolName: tc.tool})
		if err != nil {
			t.Fatalf("%s: Request error: %v", tc.tool, err)
		}
		if got.Behavior != tc.want {
			t.Errorf("%s: Behavior = %q, want %q", tc.tool, got.Behavior, tc.want)
		}
	}
}

// TestDownstream_CancelDenies confirms a cancelled context unblocks a parked
// Request with a deny — so a host that wires a broker but never responds (or
// whose UI is torn down mid-prompt) fails closed rather than hanging.
func TestDownstream_CancelDenies(t *testing.T) {
	broker := permission.NewBroker()
	// No SetOnRequest responder: the request parks until ctx is cancelled.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	got, err := broker.Request(ctx, permission.ApprovalRequest{ToolName: "bash"})
	if err == nil {
		t.Fatal("expected ctx error, got nil")
	}
	if got.Behavior != permission.BehaviorDeny {
		t.Errorf("cancelled Request should deny, got %q", got.Behavior)
	}
}
