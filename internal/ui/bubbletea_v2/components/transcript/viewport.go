package transcript

import (
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
)

// View wraps bubbles/viewport with follow-mode bookkeeping: the
// scrollback auto-snaps to the bottom on content change unless the
// user has scrolled up.
//
// M3 ships the minimum viable wrapper: SetSize, MarkDirty,
// View/Update passthroughs. M5 binds PgUp/PgDn/Home/End key handling
// (it's currently handled by viewport.Model's default bindings).
// M8 routes wheel events here.
type View struct {
	vp     viewport.Model
	tr     *Transcript
	follow bool
}

// NewView constructs a viewport wrapper for the given transcript.
// Follow mode is on by default — typical user expectation is that
// new content scrolls into view.
func NewView(tr *Transcript) *View {
	return &View{
		vp:     viewport.New(80, 20),
		tr:     tr,
		follow: true,
	}
}

// SetSize updates the viewport's display dims and the transcript's
// rendering width. Snapshots follow-mode-at-bottom across the resize
// so a layout change doesn't break "I'm reading the latest".
func (v *View) SetSize(width, height int) {
	if width < 1 || height < 1 {
		return
	}
	wasAtBottom := v.vp.AtBottom()
	v.vp.Width = width
	v.vp.Height = height
	v.tr.SetWidth(width)
	v.refresh()
	if v.follow && wasAtBottom {
		v.vp.GotoBottom()
	}
}

// MarkDirty re-renders the transcript into the viewport. Called by
// the App after every mutation that could change the visible
// content (event ingest, prompt append, banner update).
//
// Cheap when nothing changed: the block cache returns memoised
// strings, so the work is roughly proportional to the number of
// blocks.
func (v *View) MarkDirty() {
	v.refresh()
}

func (v *View) refresh() {
	wasAtBottom := v.vp.AtBottom()
	v.vp.SetContent(v.tr.View())
	if v.follow && wasAtBottom {
		v.vp.GotoBottom()
	}
}

// Update routes messages to the underlying viewport. M3 only
// handles what bubbles/viewport handles by default (PgUp/PgDn/
// Home/End). M8 will route wheel events here too.
func (v *View) Update(msg tea.Msg) tea.Cmd {
	var cmd tea.Cmd
	v.vp, cmd = v.vp.Update(msg)
	// User-driven scroll up should disable follow mode so streaming
	// chunks don't yank them back to the bottom.
	if !v.vp.AtBottom() {
		v.follow = false
	} else {
		v.follow = true
	}
	return cmd
}

// View returns the viewport's current visible window.
func (v *View) View() string {
	return v.vp.View()
}

// GotoBottom re-enables follow mode and jumps to the latest
// content. Useful for a future End key binding (M5).
func (v *View) GotoBottom() {
	v.follow = true
	v.vp.GotoBottom()
}

// Following reports follow-mode state. Test-only.
func (v *View) Following() bool { return v.follow }
