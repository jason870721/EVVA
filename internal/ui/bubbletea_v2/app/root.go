// Package app is the v2 TUI's top-level tea.Model. It stays thin on
// purpose — focus stack, layout engine, and msg dispatch live here;
// every visual concern lives in a sibling component package.
package app

import (
	"context"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/johnny1110/evva/internal/agent/event"
	"github.com/johnny1110/evva/internal/ui"
	"github.com/johnny1110/evva/internal/ui/bubbletea_v2/components/input"
	"github.com/johnny1110/evva/internal/ui/bubbletea_v2/components/transcript"
	"github.com/johnny1110/evva/internal/ui/bubbletea_v2/events"
	"github.com/johnny1110/evva/internal/ui/bubbletea_v2/theme"
	"github.com/johnny1110/evva/pkg/banner"
)

// defaultGreeting is the welcome line rendered inside the banner box
// on startup.
const defaultGreeting = "// neural link established — what shall we build, ʘᴥʘ?"

// App is the v2 root model. M4 mounts input + paste + history;
// pressing Enter on non-empty content kicks off a Run, transcript
// renders the resulting event stream. Run state is tracked minimally
// (one bool + a cancel func) — M5 brings the full RunState machine.
type App struct {
	evvaHome   string
	program    *tea.Program
	controller ui.Controller

	width  int
	height int

	theme      *theme.Theme
	transcript *transcript.Transcript
	view       *transcript.View
	input      *input.Input

	// running tracks whether an agent Run is in flight. While true,
	// Esc cancels the run instead of quitting, and a second submit
	// queues to the agent's UserPromptQueue rather than starting a
	// fresh Run (the model would 400 on a stacked turn). M5
	// replaces this with a full RunState enum.
	running   bool
	runCancel context.CancelFunc

	// hint is a one-line transient status note rendered above the
	// input (placeholder for the full status bar that lands in M5).
	hint string

	startedAt time.Time
}

// New builds a fresh App. The program reference is wired in
// afterwards (tea.NewProgram needs the model before the model can
// know about the program).
func New(evvaHome string) *App {
	th := theme.Default()
	tr := transcript.New()
	tr.SetTheme(th)
	tr.SetBanner(transcript.BannerSpec{
		Art:      banner.Load(evvaHome),
		Greeting: defaultGreeting,
	})
	v := transcript.NewView(tr)
	in := input.New(th)

	return &App{
		evvaHome:   evvaHome,
		theme:      th,
		transcript: tr,
		view:       v,
		input:      in,
		startedAt:  time.Now(),
	}
}

// SetProgram lets the package-level UI hand the model the program
// reference. Used by the run goroutine to dispatch RunDoneMsg back
// to the bubbletea main loop.
func (a *App) SetProgram(p *tea.Program) { a.program = p }

// Attach hands the model the agent controller and re-renders the
// banner with controller metadata.
func (a *App) Attach(c ui.Controller) {
	a.controller = c
	a.refreshBanner()
	a.view.MarkDirty()
}

func (a *App) refreshBanner() {
	if a.controller == nil {
		return
	}
	id := a.controller.AgentID()
	if len(id) > 8 {
		id = id[:8]
	}
	a.transcript.SetBanner(transcript.BannerSpec{
		Art:      banner.Load(a.evvaHome),
		Greeting: defaultGreeting,
		Info: []transcript.BannerInfo{
			{Label: "agent", Value: id},
			{Label: "model", Value: a.controller.Model()},
			{Label: "started", Value: a.startedAt.Format("2006-01-02 15:04:05")},
		},
	})
}

// Init returns the textarea's blink command so the cursor visibly
// animates from the first frame.
func (a *App) Init() tea.Cmd { return a.input.BlinkCmd() }

// Update routes incoming messages.
func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch m := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = m.Width, m.Height
		// Reserve 5 rows for the input box (3 textarea rows + 2
		// border lines). Remainder is the viewport. M5 will
		// reserve more rows for the status bar.
		viewportH := m.Height - 5
		if viewportH < 1 {
			viewportH = 1
		}
		a.view.SetSize(m.Width, viewportH)
		a.input.SetWidth(m.Width)
		return a, nil

	case events.QuitMsg:
		if a.runCancel != nil {
			a.runCancel()
		}
		return a, tea.Quit

	case events.AgentEventMsg:
		if a.transcript.IngestEvent(m.Event) {
			a.view.MarkDirty()
		}
		return a, nil

	case events.RunDoneMsg:
		a.running = false
		a.runCancel = nil
		if m.Err != nil {
			a.hint = m.Err.Error()
		}
		return a, nil

	case input.SubmitMsg:
		return a.handleSubmit(m)

	case tea.KeyMsg:
		return a.handleKey(m)
	}
	return a, nil
}

// handleKey routes a key event. The order matters: special keys
// (quit, scroll, expand, history) take precedence over the input's
// textarea so multi-line composition with embedded special chars
// behaves consistently.
func (a *App) handleKey(m tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.String() {
	case "ctrl+c":
		// Idle: quit. Running: cancel the run.
		if a.running && a.runCancel != nil {
			a.runCancel()
			a.hint = "interrupted"
			return a, nil
		}
		return a, tea.Quit

	case "esc":
		// Esc cancels an in-flight run; otherwise quit. M5 will
		// extend this to dismiss overlays first.
		if a.running && a.runCancel != nil {
			a.runCancel()
			a.hint = "interrupted"
			return a, nil
		}
		return a, tea.Quit

	case "ctrl+o":
		// Toggle the transcript's fold/expand state.
		a.transcript.ToggleExpand()
		a.view.MarkDirty()
		return a, nil

	case "pgup", "pgdown", "home", "end":
		// Forward to the viewport for scroll. Multi-line input
		// composition doesn't use these.
		return a, a.view.Update(m)
	}

	// Everything else falls through to the input textarea —
	// including bracketed-paste KeyMsgs (Paste=true).
	cmd := a.input.Update(m)
	return a, cmd
}

// handleSubmit dispatches a SubmitMsg from the Input. Three paths:
//   - empty submit  → no-op (M5 will route this to iter-limit continue)
//   - slash command → handle inline (M4 ships /exit /quit /clear;
//                     /config, /model, /compact land in M7)
//   - regular text  → append to transcript, start a Run (or queue
//                     if one is already in flight)
func (a *App) handleSubmit(m input.SubmitMsg) (tea.Model, tea.Cmd) {
	text := strings.TrimSpace(m.ForAgent)

	// Slash commands.
	switch text {
	case "/exit", "/quit", "exit":
		a.input.Reset()
		return a, tea.Quit
	case "/clear":
		a.transcript.Reset()
		a.input.Reset()
		a.view.MarkDirty()
		return a, nil
	}

	if text == "" {
		// M5 will check for iter-limit pause and Continue here.
		// For M4, empty submit is a no-op.
		return a, nil
	}

	if a.controller == nil {
		a.hint = "no controller attached"
		return a, nil
	}

	// Mid-run submit: queue the prompt for the agent to drain at
	// the top of its next iteration. Starting a second Run while
	// one is in flight would append a RoleUser between an
	// unanswered tool_calls turn and its pending tool_result;
	// every provider 400s on that shape.
	if a.running {
		a.transcript.AppendUserPrompt(m.ForView)
		a.input.Reset()
		a.controller.ToolState().UserPromptQueue().Enqueue(m.ForAgent)
		a.hint = "queued — will land at the next iteration"
		a.view.MarkDirty()
		return a, nil
	}

	a.transcript.AppendUserPrompt(m.ForView)
	a.input.Reset()
	a.view.MarkDirty()
	a.startRun(m.ForAgent)
	return a, nil
}

// startRun kicks off a goroutine that drives controller.Run and
// reports completion via RunDoneMsg. Captures program at call time
// so a SetProgram-after-Run race is impossible (the goroutine
// closes over whatever pointer existed when it started).
func (a *App) startRun(prompt string) {
	if a.controller == nil {
		return
	}
	ctx, cancel := context.WithCancel(context.Background())
	a.runCancel = cancel
	a.running = true
	a.hint = ""

	p := a.program
	go func() {
		_, err := a.controller.Run(ctx, prompt)
		if p != nil {
			p.Send(events.RunDoneMsg{Err: err})
		}
	}()
}

// View composes the rendered output: transcript viewport on top,
// optional hint, input box on the bottom.
func (a *App) View() string {
	var b strings.Builder
	b.WriteString(a.view.View())
	b.WriteByte('\n')
	if a.hint != "" {
		b.WriteString(a.theme.DimText.Render("  " + a.hint))
		b.WriteByte('\n')
	}
	b.WriteString(a.input.View())
	return b.String()
}

// IngestEvent / SuppressUnusedHint — exported only so test code can
// drive App-level scenarios in M5+. No external callers in M4.
var _ event.Event // silence import-unused while only the App.Update branch consumes events.Event
