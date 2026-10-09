package ui

import (
	"fmt"
	"time"

	"github.com/charmbracelet/bubbles/table"
	tea "github.com/charmbracelet/bubbletea"

	"nmail/internal/mailbox"
)

const (
	colFlag = 1
	colDate = 12
	colFrom = 24
)

type inboxModel struct {
	table    table.Model
	messages []mailbox.Summary
}

func newInbox() inboxModel {
	m := inboxModel{table: table.New(table.WithFocused(true))}
	m.setSize(80, 24) // until the first WindowSizeMsg
	return m
}

func (m *inboxModel) setSize(w, h int) {
	subject := max(w-colFlag-colDate-colFrom-8, 10)
	m.table.SetColumns([]table.Column{
		{Title: "", Width: colFlag},
		{Title: "Date", Width: colDate},
		{Title: "From", Width: colFrom},
		{Title: "Subject", Width: subject},
	})
	m.table.SetWidth(w)
	m.table.SetHeight(max(h-4, 3))
}

func (m *inboxModel) setMessages(msgs []mailbox.Summary) {
	m.updateMessages(msgs)
	m.table.SetCursor(0)
}

// updateMessages swaps in msgs, keeping the cursor where it was.
func (m *inboxModel) updateMessages(msgs []mailbox.Summary) {
	m.messages = msgs
	rows := make([]table.Row, len(msgs))
	for i, s := range msgs {
		flag := "●"
		if s.Seen {
			flag = " "
		}
		rows[i] = table.Row{flag, formatDate(s.Date), s.From, s.Subject}
	}
	m.table.SetRows(rows)
	m.table.SetCursor(min(m.table.Cursor(), max(len(msgs)-1, 0)))
}

func (m inboxModel) selected() (mailbox.Summary, bool) {
	i := m.table.Cursor()
	if i < 0 || i >= len(m.messages) {
		return mailbox.Summary{}, false
	}
	return m.messages[i], true
}

func (m inboxModel) Update(msg tea.Msg) (inboxModel, tea.Cmd) {
	var cmd tea.Cmd
	m.table, cmd = m.table.Update(msg)
	return m, cmd
}

// View renders the inbox with the account switcher beside the title.
func (m inboxModel) View(tabs string) string {
	title := titleStyle.Render(fmt.Sprintf("INBOX — %d messages", len(m.messages)))
	if tabs != "" {
		title += "  " + tabs
	}
	help := helpStyle.Render("↑↓/jk move • enter open • d delete • r refresh • tab/1-9 switch • a add account • L log out • q quit")
	return title + "\n" + m.table.View() + "\n" + help
}

func formatDate(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	t = t.Local()
	now := time.Now()
	switch {
	case t.YearDay() == now.YearDay() && t.Year() == now.Year():
		return t.Format("15:04")
	case t.Year() == now.Year():
		return t.Format("Jan 02")
	default:
		return t.Format("2006-01-02")
	}
}
