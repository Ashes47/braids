package tui

import (
	"fmt"

	"strings"
	"time"

	"charm.land/lipgloss/v2"

	"github.com/Ashes47/braids/internal/core/trash"
)

// binState is the list of deleted conversations.
//
// Undo alone was not enough: it reached one step back, and only for as long as
// the session lived. Recovering something deleted two days ago means being able
// to look at what was deleted, which means a screen rather than a keystroke.
type binState struct {
	entries []trash.Entry
	cursor  int
	offset  int
	err     error
	notice  string
	failed  bool
}

const (
	binWhenWidth    = 14
	binSizeWidth    = 9
	binExpiryWidth  = 12
	binConfirmDelay = time.Second
)

func (m Model) openBin() Model {
	if m.loadBin == nil {
		return m.withNotice("the bin is unavailable", true)
	}
	entries, err := m.loadBin()
	m.bin = &binState{entries: entries, err: err}
	m.returnTo = m.mode
	m.mode = binMode
	return m
}

func (m Model) binKey(key string) Model {
	b := m.bin
	switch key {
	case "esc", "backspace", "h", "left":
		m.mode = m.returnTo
		m.bin = nil
	case "j", "down":
		b.cursor = wrap(b.cursor, 1, len(b.entries))
	case "k", "up":
		b.cursor = wrap(b.cursor, -1, len(b.entries))
	case "g", "home":
		b.cursor = 0
	case "G", "end":
		b.cursor = len(b.entries) - 1
	case "enter", "r":
		return m.restoreFromBin()
	case "d":
		return m.purgeFromBin()
	}
	m.clampBin()
	return m
}

func (m *Model) clampBin() {
	b := m.bin
	if b == nil {
		return
	}
	b.cursor = min(max(b.cursor, 0), max(len(b.entries)-1, 0))
	h := m.bodyHeight()
	if b.cursor < b.offset {
		b.offset = b.cursor
	}
	if b.cursor >= b.offset+h {
		b.offset = b.cursor - h + 1
	}
	b.offset = max(b.offset, 0)
}

func (m Model) restoreFromBin() Model {
	b := m.bin
	if b.cursor >= len(b.entries) || m.restoreFn == nil {
		return m
	}
	entry := b.entries[b.cursor]
	if err := m.restoreFn(entry.ID); err != nil {
		b.notice, b.failed = err.Error(), true
		return m
	}
	m = m.catchUp()
	m.bin.entries = remove(m.bin.entries, entry.ID)
	m.bin.notice, m.bin.failed = fmt.Sprintf("restored %s · it is back on the map", entry.Label), false
	m.clampBin()
	return m
}

func (m Model) purgeFromBin() Model {
	b := m.bin
	if b.cursor >= len(b.entries) || m.purgeFn == nil {
		return m
	}
	entry := b.entries[b.cursor]
	if err := m.purgeFn(entry.ID); err != nil {
		b.notice, b.failed = err.Error(), true
		return m
	}
	b.entries = remove(b.entries, entry.ID)
	b.notice, b.failed = fmt.Sprintf("%s is gone for good", entry.Label), false
	m.clampBin()
	return m
}

func remove(entries []trash.Entry, id string) []trash.Entry {
	out := entries[:0]
	for _, e := range entries {
		if e.ID != id {
			out = append(out, e)
		}
	}
	return out
}

func (m Model) renderBin() string {
	b := m.bin
	var out strings.Builder
	out.WriteString(m.binInfo())
	out.WriteString("\n\n")
	out.WriteString(m.panelTopTitled(fmt.Sprintf("Deleted[%d]", len(b.entries))))
	out.WriteString("\n")
	out.WriteString(m.framed(m.binColumns()))
	out.WriteString("\n")

	blank := repeat(" ", m.contentWidth())
	switch {
	case b.err != nil:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render(b.err.Error()), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	case len(b.entries) == 0:
		out.WriteString(m.framed(padRight(" "+m.theme.Empty.Render("nothing has been deleted"), m.contentWidth())) + "\n")
		m.fill(&out, blank, m.bodyHeight()-1)
	default:
		end := min(b.offset+m.bodyHeight(), len(b.entries))
		for i := b.offset; i < end; i++ {
			out.WriteString(m.framed(m.renderBinRow(b.entries[i], i == b.cursor)) + "\n")
		}
		m.fill(&out, blank, m.bodyHeight()-(end-b.offset))
	}
	out.WriteString(m.panelBottom())
	out.WriteString("\n")
	if b.notice != "" {
		out.WriteString(" " + m.noticeStyle(b.failed).Render(truncate(b.notice, m.width-2)))
	}
	return out.String()
}

func (m Model) fill(out *strings.Builder, blank string, n int) {
	for range n {
		out.WriteString(m.framed(blank) + "\n")
	}
}

func (m Model) binFacts() []fact {
	var bytes int64
	soonest := ""
	for _, e := range m.bin.entries {
		bytes += e.Bytes
	}
	if n := len(m.bin.entries); n > 0 {
		soonest = expiryOf(m.bin.entries[n-1], m.now())
	}
	return []fact{
		{"Deleted", fmt.Sprintf("%d", len(m.bin.entries))},
		{"Holding", humanBytes(bytes)},
		{"Kept for", fmt.Sprintf("%d days", int(trash.Retention.Hours()/24))},
		{"Next to go", orDash(soonest)},
	}
}

func binHints() []hint {
	return []hint{
		{"j/k", "down / up"}, {"↵ / r", "restore"},
		{"d", "delete for good"}, {"esc", "back"},
	}
}

func (m Model) binInfo() string { return m.factsBlock(m.binFacts(), binHints(), nil) }

func (m Model) binColumns() string {
	nameWidth := m.binNameWidth()
	return m.theme.Column.Render(" " + padRight("CONVERSATION", nameWidth) + " " +
		padLeft("DELETED", binWhenWidth) + " " + padLeft("SIZE", binSizeWidth) + " " +
		padLeft("EXPIRES", binExpiryWidth))
}

func (m Model) binNameWidth() int {
	return max(m.contentWidth()-4-binWhenWidth-binSizeWidth-binExpiryWidth, 8)
}

func (m Model) renderBinRow(entry trash.Entry, selected bool) string {
	name := padRight(truncate(entry.Label, m.binNameWidth()), m.binNameWidth())
	when := padLeft(humanAge(m.now().Sub(entry.At))+" ago", binWhenWidth)
	size := padLeft(humanBytes(entry.Bytes), binSizeWidth)
	expiry := padLeft(expiryOf(entry, m.now()), binExpiryWidth)

	plain := " " + name + " " + when + " " + size + " " + expiry
	if selected {
		return m.theme.Selected.Width(m.contentWidth()).Render(plain)
	}
	return " " + m.theme.Value.Render(name) + " " + m.theme.Faint.Render(when) + " " +
		m.theme.Faint.Render(size) + " " + m.expiryStyle(entry).Render(expiry)
}

// expiryOf says how long is left before an entry goes for good.
func expiryOf(entry trash.Entry, now time.Time) string {
	left := entry.Expires().Sub(now)
	if left <= 0 {
		return "any moment"
	}
	return "in " + humanAge(left)
}

// expiryStyle warns as the deadline approaches, so a conversation does not
// quietly pass the point of recovery.
func (m Model) expiryStyle(entry trash.Entry) lipgloss.Style {
	if entry.Expires().Sub(m.now()) < 48*time.Hour {
		return m.theme.Accent
	}
	return m.theme.Faint
}
