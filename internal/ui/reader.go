package ui

import (
	"os/exec"
	"runtime"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"

	"nmail/internal/mailbox"
	"nmail/internal/render"
)

const readerHeaderLines = 5

type readerModel struct {
	viewport viewport.Model
	summary  mailbox.Summary
	header   string
	width    int
	doc      render.Doc
	rule     string // the render rule that matched, "" for the default
	focus    int    // focused link, -1 for none
	starts   []int  // the line each link starts on
}

func newReader() readerModel {
	return readerModel{viewport: viewport.New(80, 20), focus: -1}
}

func (m *readerModel) setSize(w, h int) {
	m.width = w
	m.viewport.Width = w
	m.viewport.Height = max(h-readerHeaderLines-3, 3)
	m.render()
}

func (m *readerModel) show(msg *mailbox.Message) {
	m.summary = msg.Summary
	m.header = strings.Join([]string{
		titleStyle.Render(msg.Subject),
		labelStyle.Render("From: ") + msg.From,
		labelStyle.Render("To:   ") + msg.To,
		labelStyle.Render("Date: ") + msg.Date.Local().Format("Mon, 02 Jan 2006 15:04"),
	}, "\n")
	m.doc, m.rule = render.Message(msg)
	m.focus = -1
	m.render()
	m.viewport.GotoTop()
}

func (m *readerModel) render() {
	var content string
	content, m.starts = m.doc.Render(max(m.width, 20), m.focus)
	m.viewport.SetContent(content)
}

// step moves link focus by delta. From no focus it picks the first link
// at or after the top of the screen (or the last before its bottom).
func (m *readerModel) step(delta int) {
	n := len(m.doc.Links)
	if n == 0 {
		return
	}
	if m.focus < 0 {
		m.focus = m.firstVisible(delta)
	} else {
		m.focus = (m.focus + delta + n) % n
	}
	m.render()
	if line := m.starts[m.focus]; line >= 0 {
		top := m.viewport.YOffset
		if line < top || line >= top+m.viewport.Height {
			m.viewport.SetYOffset(max(line-m.viewport.Height/3, 0))
		}
	}
}

func (m *readerModel) firstVisible(delta int) int {
	top, bottom := m.viewport.YOffset, m.viewport.YOffset+m.viewport.Height
	if delta > 0 {
		for i, line := range m.starts {
			if line >= top {
				return i
			}
		}
		return 0
	}
	for i := len(m.starts) - 1; i >= 0; i-- {
		if m.starts[i] >= 0 && m.starts[i] < bottom {
			return i
		}
	}
	return len(m.starts) - 1
}

func (m *readerModel) blur() bool {
	if m.focus < 0 {
		return false
	}
	m.focus = -1
	m.render()
	return true
}

func (m readerModel) focused() (render.Link, bool) {
	if m.focus < 0 || m.focus >= len(m.doc.Links) {
		return render.Link{}, false
	}
	return m.doc.Links[m.focus], true
}

func (m readerModel) Update(msg tea.Msg) (readerModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m readerModel) View() string {
	status := ""
	if l, ok := m.focused(); ok {
		status = labelStyle.Render("→ ") + l.URL
	} else if m.rule != "" {
		status = labelStyle.Render("rendered as " + m.rule)
	}
	help := "↑↓/pgup/pgdn scroll • d delete • esc/q back"
	if len(m.doc.Links) > 0 {
		help = "↑↓/pgup/pgdn scroll • tab/shift+tab links • enter open • d delete • esc/q back"
	}
	return m.header + "\n\n" + m.viewport.View() + "\n" + truncate(status, m.width) + "\n" + helpStyle.Render(help)
}

func truncate(s string, w int) string {
	if w <= 0 || len([]rune(s)) <= w {
		return s
	}
	return string([]rune(s)[:max(w-1, 0)]) + "…"
}

type openedMsg struct{ err error }

// openURL hands a link to the system browser or mail client.
var openURL = func(url string) error {
	name := "xdg-open"
	if runtime.GOOS == "darwin" {
		name = "open"
	}
	return exec.Command(name, url).Run()
}

func open(l render.Link) tea.Cmd {
	return func() tea.Msg { return openedMsg{err: openURL(l.URL)} }
}
