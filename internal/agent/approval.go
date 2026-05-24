package agent

import (
	"github.com/johnny1110/evva/internal/question"
	"github.com/johnny1110/evva/pkg/event"
	"github.com/johnny1110/evva/pkg/permission"
)

// wireBrokers installs the agent's default permission + question brokers and
// the bridge that turns their requests into sink events. It runs once, on the
// root agent only: subagents inherit the root's already-wired brokers through
// spawn.go, so re-registering here would point the shared callback at a
// subagent's sink.
//
// The rule per broker:
//
//   - host supplied one (WithPermissionBroker / WithQuestionBroker) → leave it
//     untouched; the host owns its OnRequest callback (e.g. cmd/evva's CLI
//     deny path, or a downstream allow/deny policy).
//   - none + a sink is present → construct a default broker and emit the
//     request to the sink. This is what frees an interactive host from
//     hand-wiring buildApprovalEvent / SetOnRequest.
//   - none + no sink → construct a default broker that auto-denies (the safe
//     headless default for programmatic hosts with no approval surface).
func wireBrokers(a *Agent) {
	if a.IsSubagent() {
		return
	}
	wirePermissionBroker(a)
	wireQuestionBroker(a)
}

func wirePermissionBroker(a *Agent) {
	if a.permissionBroker != nil {
		return
	}
	b := permission.NewBroker()
	a.permissionBroker = b
	if a.sink != nil {
		permission.SetOnRequest(b, func(req permission.ApprovalRequest) {
			a.sink.Emit(buildApprovalEvent(req))
		})
		return
	}
	permission.SetOnRequest(b, func(req permission.ApprovalRequest) {
		_ = b.Respond(req.ID, permission.Decision{
			Behavior: permission.BehaviorDeny,
			Reason:   "no approval surface installed; pass a sink or agent.WithPermissionBroker",
		})
	})
}

func wireQuestionBroker(a *Agent) {
	if a.questionBroker != nil {
		return
	}
	b := question.NewBroker()
	a.questionBroker = b
	if a.sink != nil {
		question.SetOnRequest(b, func(req question.Request) {
			a.sink.Emit(buildQuestionEvent(req))
		})
		return
	}
	question.SetOnRequest(b, func(req question.Request) {
		_ = b.Respond(req.ID, question.Response{})
	})
}

// buildApprovalEvent converts a permission.ApprovalRequest into the
// KindApprovalNeeded event the UI subscribes to. AgentID is carried from the
// request (not the emitting agent) so a subagent's prompt stays attributed to
// the subagent even though the root agent owns the broker callback.
func buildApprovalEvent(req permission.ApprovalRequest) event.Event {
	riskHint := ""
	switch {
	case req.Hint.IsDangerous:
		riskHint = "dangerous"
	case req.Hint.IsReadOnly:
		riskHint = "read-only"
	default:
		riskHint = "unknown"
	}
	return event.Event{
		Kind:    event.KindApprovalNeeded,
		AgentID: req.AgentID,
		ApprovalNeeded: &event.ApprovalNeededPayload{
			RequestID:        req.ID,
			ToolName:         req.ToolName,
			ToolInput:        req.ToolInput,
			InputDescription: req.InputDescription,
			Mode:             string(req.Mode),
			Reason:           req.Reason,
			RiskHint:         riskHint,
			Matched:          req.Hint.Matched,
			PlanContent:      req.PlanContent,
		},
	}
}

// buildQuestionEvent converts a question.Request into the KindQuestionNeeded
// event the UI subscribes to.
func buildQuestionEvent(req question.Request) event.Event {
	items := make([]event.QuestionItem, len(req.Questions))
	for i, q := range req.Questions {
		opts := make([]event.QuestionOption, len(q.Options))
		for j, o := range q.Options {
			opts[j] = event.QuestionOption{Label: o.Label, Description: o.Description, Preview: o.Preview}
		}
		items[i] = event.QuestionItem{
			Question:    q.Question,
			Header:      q.Header,
			MultiSelect: q.MultiSelect,
			Options:     opts,
		}
	}
	return event.Event{
		Kind:    event.KindQuestionNeeded,
		AgentID: req.AgentID,
		QuestionNeeded: &event.QuestionNeededPayload{
			RequestID: req.ID,
			AgentID:   req.AgentID,
			Questions: items,
		},
	}
}
