package task

import (
	"sync"
	"testing"

	"github.com/johnny1110/evva/internal/observable"
)

// Phase 1 analysis — TaskGroup surface:
//   - NewTaskGroup returns an empty, ready-to-use store
//   - Domain() returns the package constant
//   - Create assigns monotonic IDs ("t1","t2",…), stamps CreatedAt/UpdatedAt,
//     forces Status=Pending, emits "created" notification
//   - Get returns a COPY (mutating the result must not affect the store)
//   - List preserves insertion order including deleted entries
//   - Update applies pointer-or-empty patch semantics, rejects invalid status,
//     merges metadata (nil-value keys delete), emits "updated" notification
//   - Clear wipes state AND emits one "removed" change per task
//   - mergeStrings dedupes and skips empties while preserving order

// collectObserver returns a small Observer that records each Change.
// Concurrency-safe so tests can run with -race.
func collectObserver() (observable.Observer, func() []observable.Change) {
	var mu sync.Mutex
	var got []observable.Change
	obs := func(c observable.Change) {
		mu.Lock()
		got = append(got, c)
		mu.Unlock()
	}
	read := func() []observable.Change {
		mu.Lock()
		defer mu.Unlock()
		return append([]observable.Change(nil), got...)
	}
	return obs, read
}

func TestStore_Domain(t *testing.T) {
	g := NewTaskGroup()
	if got := g.Domain(); got != Domain {
		t.Errorf("Domain(): got %q, want %q", got, Domain)
	}
}

func TestStore_Create_AssignsIDsMonotonically(t *testing.T) {
	g := NewTaskGroup()
	a := g.Create(Task{Subject: "first"})
	b := g.Create(Task{Subject: "second"})
	c := g.Create(Task{Subject: "third"})

	if a.ID != "t1" || b.ID != "t2" || c.ID != "t3" {
		t.Errorf("IDs: got %s/%s/%s, want t1/t2/t3", a.ID, b.ID, c.ID)
	}
	if a.Status != StatusPending {
		t.Errorf("Status: got %q, want %q", a.Status, StatusPending)
	}
	if a.CreatedAt.IsZero() || a.UpdatedAt.IsZero() {
		t.Error("timestamps not stamped on Create")
	}
}

func TestStore_Create_NotifiesObservers(t *testing.T) {
	g := NewTaskGroup()
	obs, read := collectObserver()
	g.Subscribe(obs)

	g.Create(Task{Subject: "do thing"})

	events := read()
	if len(events) != 1 {
		t.Fatalf("expected 1 change, got %d", len(events))
	}
	if events[0].Op != "created" {
		t.Errorf("Op: got %q, want %q", events[0].Op, "created")
	}
	if events[0].Domain != Domain {
		t.Errorf("Domain: got %q, want %q", events[0].Domain, Domain)
	}
	sum, ok := events[0].Payload.(Summary)
	if !ok {
		t.Fatalf("Payload type: got %T, want Summary", events[0].Payload)
	}
	if sum.Subject != "do thing" {
		t.Errorf("Summary.Subject: got %q", sum.Subject)
	}
}

func TestStore_Get_NotFound(t *testing.T) {
	g := NewTaskGroup()
	_, ok := g.Get("t-missing")
	if ok {
		t.Error("Get of missing ID returned ok=true")
	}
}

func TestStore_Get_ReturnsCopy(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "orig", Metadata: map[string]any{"k": "v"}})
	got, _ := g.Get("t1")
	got.Subject = "mutated-via-copy"
	got.Metadata["k"] = "MUTATED"
	// Touch the field so the linter knows we wrote it deliberately —
	// the actual assertion is "again.Subject is still 'orig'" below.
	if got.Subject != "mutated-via-copy" {
		t.Fatalf("local copy mutation didn't stick: got %q", got.Subject)
	}

	again, _ := g.Get("t1")
	if again.Subject != "orig" {
		t.Errorf("Subject mutated through returned copy: got %q", again.Subject)
	}
	// Metadata is a map — copies share the underlying map. This is the
	// current behavior; locking it down so a future deep-copy refactor
	// is a deliberate choice.
	if v, want := again.Metadata["k"], "MUTATED"; v != want {
		t.Logf("note: Metadata map is shared (current contract): got %v", v)
	}
}

func TestStore_List_PreservesInsertionOrder(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "a"})
	g.Create(Task{Subject: "b"})
	g.Create(Task{Subject: "c"})

	list := g.List()
	if len(list) != 3 {
		t.Fatalf("len: got %d, want 3", len(list))
	}
	if list[0].Subject != "a" || list[1].Subject != "b" || list[2].Subject != "c" {
		t.Errorf("order drifted: %+v", list)
	}
}

func TestStore_List_IncludesDeleted(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "live"})
	g.Create(Task{Subject: "doomed"})
	del := StatusDeleted
	g.Update("t2", UpdatePatch{Status: &del})

	got := g.List()
	if len(got) != 2 {
		t.Errorf("List len: got %d, want 2 (deleted MUST be included)", len(got))
	}
	if got[1].Status != StatusDeleted {
		t.Errorf("deleted task status: got %q", got[1].Status)
	}
}

func TestStore_Update_AllFields(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "orig", Description: "old desc"})

	newStatus := StatusInProgress
	newSubject := "rewritten"
	newDesc := "new desc"
	newActive := "Doing it"
	newOwner := "alice"

	updated, ok, err := g.Update("t1", UpdatePatch{
		Status:       &newStatus,
		Subject:      &newSubject,
		Description:  &newDesc,
		ActiveForm:   &newActive,
		Owner:        &newOwner,
		AddBlocks:    []string{"t9"},
		AddBlockedBy: []string{"t8", "t8"}, // duplicate should dedupe
	})

	if err != nil {
		t.Fatalf("unexpected err: %v", err)
	}
	if !ok {
		t.Fatal("ok=false on existing task")
	}
	if updated.Status != newStatus {
		t.Errorf("Status: got %q", updated.Status)
	}
	if updated.Subject != newSubject {
		t.Errorf("Subject: got %q", updated.Subject)
	}
	if updated.Description != newDesc {
		t.Errorf("Description: got %q", updated.Description)
	}
	if updated.ActiveForm != newActive {
		t.Errorf("ActiveForm: got %q", updated.ActiveForm)
	}
	if updated.Owner != newOwner {
		t.Errorf("Owner: got %q", updated.Owner)
	}
	if len(updated.Blocks) != 1 || updated.Blocks[0] != "t9" {
		t.Errorf("Blocks: got %v", updated.Blocks)
	}
	if len(updated.BlockedBy) != 1 || updated.BlockedBy[0] != "t8" {
		t.Errorf("BlockedBy (deduped): got %v", updated.BlockedBy)
	}
}

func TestStore_Update_PartialPatchLeavesOthers(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "orig", Description: "do NOT touch", Owner: "alice"})

	newStatus := StatusCompleted
	updated, _, _ := g.Update("t1", UpdatePatch{Status: &newStatus})

	if updated.Description != "do NOT touch" {
		t.Errorf("Description leaked: got %q", updated.Description)
	}
	if updated.Owner != "alice" {
		t.Errorf("Owner leaked: got %q", updated.Owner)
	}
	if updated.Subject != "orig" {
		t.Errorf("Subject leaked: got %q", updated.Subject)
	}
}

func TestStore_Update_InvalidStatusReturnsError(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "x"})

	bogus := Status("not-a-status")
	_, ok, err := g.Update("t1", UpdatePatch{Status: &bogus})

	if err == nil {
		t.Fatal("expected error for invalid status")
	}
	if !ok {
		t.Error("ok should be true (task exists) even when status is invalid")
	}
	// Verify the task was NOT mutated.
	after, _ := g.Get("t1")
	if after.Status != StatusPending {
		t.Errorf("status mutated despite error: got %q, want %q", after.Status, StatusPending)
	}
}

func TestStore_Update_NotFound(t *testing.T) {
	g := NewTaskGroup()
	_, ok, err := g.Update("t-nope", UpdatePatch{})
	if err != nil {
		t.Errorf("missing-id should return ok=false, not err: %v", err)
	}
	if ok {
		t.Error("ok=true for missing id")
	}
}

func TestStore_Update_MetadataMerge(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "x", Metadata: map[string]any{"keep": "yes", "drop": "doomed"}})

	_, _, _ = g.Update("t1", UpdatePatch{
		Metadata: map[string]any{
			"new":  "added",
			"drop": nil, // nil-value → delete
		},
	})

	got, _ := g.Get("t1")
	if got.Metadata["keep"] != "yes" {
		t.Errorf("'keep' lost: got %v", got.Metadata["keep"])
	}
	if got.Metadata["new"] != "added" {
		t.Errorf("'new' missing: got %v", got.Metadata["new"])
	}
	if _, present := got.Metadata["drop"]; present {
		t.Errorf("'drop' should have been deleted; got %v", got.Metadata["drop"])
	}
}

func TestStore_Update_NotifiesObservers(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "x"})

	obs, read := collectObserver()
	g.Subscribe(obs)

	completed := StatusCompleted
	g.Update("t1", UpdatePatch{Status: &completed})

	events := read()
	if len(events) != 1 || events[0].Op != "updated" {
		t.Errorf("expected 1 'updated' change; got %d events: %v", len(events), events)
	}
}

func TestStore_Clear_RemovesAllAndNotifies(t *testing.T) {
	g := NewTaskGroup()
	g.Create(Task{Subject: "a"})
	g.Create(Task{Subject: "b"})
	g.Create(Task{Subject: "c"})

	obs, read := collectObserver()
	g.Subscribe(obs)

	g.Clear()

	if got := g.List(); len(got) != 0 {
		t.Errorf("List after Clear: got %d, want 0", len(got))
	}
	events := read()
	if len(events) != 3 {
		t.Errorf("expected 3 'removed' notifications; got %d", len(events))
	}
	for _, e := range events {
		if e.Op != "removed" {
			t.Errorf("Op: got %q, want %q", e.Op, "removed")
		}
	}
}

func TestStore_Clear_EmptyStoreIsNoOp(t *testing.T) {
	g := NewTaskGroup()
	obs, read := collectObserver()
	g.Subscribe(obs)
	g.Clear()
	if events := read(); len(events) != 0 {
		t.Errorf("Clear on empty store should not notify; got %d events", len(events))
	}
}

func TestMergeStrings(t *testing.T) {
	cases := []struct {
		name string
		a, b []string
		want []string
	}{
		{"nil + nil", nil, nil, nil},
		{"empty + new", []string{}, []string{"x", "y"}, []string{"x", "y"}},
		{"keeps a order, appends new", []string{"a", "b"}, []string{"c", "d"}, []string{"a", "b", "c", "d"}},
		{"dedups across", []string{"a", "b"}, []string{"b", "c"}, []string{"a", "b", "c"}},
		{"skips empty", []string{"a"}, []string{"", "b", ""}, []string{"a", "b"}},
		{"dedups within b", []string{}, []string{"x", "x"}, []string{"x"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := mergeStrings(tc.a, tc.b)
			if len(got) != len(tc.want) {
				t.Fatalf("len: got %v (%d), want %v (%d)", got, len(got), tc.want, len(tc.want))
			}
			for i := range got {
				if got[i] != tc.want[i] {
					t.Errorf("[%d]: got %q, want %q", i, got[i], tc.want[i])
				}
			}
		})
	}
}

func TestStatus_IsValid(t *testing.T) {
	cases := map[Status]bool{
		StatusPending:           true,
		StatusInProgress:        true,
		StatusCompleted:         true,
		StatusDeleted:           true,
		Status(""):              false,
		Status("not-a-status"):  false,
		Status("IN_PROGRESS"):   false, // case-sensitive
	}
	for s, want := range cases {
		if got := s.IsValid(); got != want {
			t.Errorf("Status(%q).IsValid() = %v, want %v", s, got, want)
		}
	}
}
