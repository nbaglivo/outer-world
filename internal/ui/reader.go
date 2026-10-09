package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"nmail/internal/mailbox"
)

const readerHeaderLines = 5

type readerModel struct {
	viewport viewport.Model
	summary  mailbox.Summary
	header   string
	width    int
	body     string
}

func newReader() readerModel {
	return readerModel{viewport: viewport.New(80, 20)}
}

func (m *readerModel) setSize(w, h int) {
	m.width = w
	m.viewport.Width = w
	m.viewport.Height = max(h-readerHeaderLines-2, 3)
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
	m.body = strings.ReplaceAll(msg.Body, "\r\n", "\n")
	m.render()
	m.viewport.GotoTop()
}

func (m *readerModel) render() {
	if m.width <= 0 {
		m.viewport.SetContent(m.body)
		return
	}
	m.viewport.SetContent(lipgloss.NewStyle().Width(m.width).Render(m.body))
}

func (m readerModel) Update(msg tea.Msg) (readerModel, tea.Cmd) {
	var cmd tea.Cmd
	m.viewport, cmd = m.viewport.Update(msg)
	return m, cmd
}

func (m readerModel) View() string {
	help := helpStyle.Render("↑↓/pgup/pgdn scroll • d delete • esc/q back")
	return m.header + "\n\n" + m.viewport.View() + "\n" + help
}
