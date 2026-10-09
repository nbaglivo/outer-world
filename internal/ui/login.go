package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"

	"nmail/internal/mailbox"
)

const (
	fieldHost = iota
	fieldPort
	fieldUser
	fieldPass
	fieldCount
	focusRemember = fieldCount
	focusCount    = fieldCount + 1
)

var fieldLabels = [fieldCount]string{"Host", "Port", "Username", "Password"}

type loginModel struct {
	inputs   [fieldCount]textinput.Model
	focus    int
	remember bool
}

// newLogin builds the form, prefilled with whatever defaults are set.
func newLogin(defaults mailbox.Credentials) loginModel {
	m := loginModel{remember: true}
	for i := range m.inputs {
		t := textinput.New()
		t.Prompt = ""
		t.CharLimit = 256
		t.Width = 40
		m.inputs[i] = t
	}
	m.inputs[fieldHost].Placeholder = "imap.example.com"
	m.inputs[fieldHost].SetValue(defaults.Host)
	m.inputs[fieldPort].SetValue(defaults.Port)
	m.inputs[fieldUser].SetValue(defaults.Username)
	m.inputs[fieldUser].Placeholder = "you@example.com"
	m.inputs[fieldPass].SetValue(defaults.Password)
	m.inputs[fieldPass].EchoMode = textinput.EchoPassword
	m.inputs[fieldPass].EchoCharacter = '•'
	m.focus = fieldPass
	for i := fieldPass; i >= fieldHost; i-- {
		if m.inputs[i].Value() == "" {
			m.focus = i
		}
	}
	m.inputs[m.focus].Focus()
	return m
}

func (m loginModel) Init() tea.Cmd {
	return textinput.Blink
}

// Update returns the credentials and true when the form is submitted.
func (m *loginModel) Update(msg tea.Msg) (mailbox.Credentials, bool, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "tab", "down":
			return mailbox.Credentials{}, false, m.setFocus(m.focus + 1)
		case "shift+tab", "up":
			return mailbox.Credentials{}, false, m.setFocus(m.focus - 1)
		case "enter":
			if m.focus < fieldPass {
				return mailbox.Credentials{}, false, m.setFocus(m.focus + 1)
			}
			if c, ok := m.credentials(); ok {
				return c, true, nil
			}
			return mailbox.Credentials{}, false, nil
		case " ", "x":
			if m.focus == focusRemember {
				m.remember = !m.remember
				return mailbox.Credentials{}, false, nil
			}
		}
	}
	if m.focus == focusRemember {
		return mailbox.Credentials{}, false, nil
	}
	var cmd tea.Cmd
	m.inputs[m.focus], cmd = m.inputs[m.focus].Update(msg)
	return mailbox.Credentials{}, false, cmd
}

func (m *loginModel) setFocus(i int) tea.Cmd {
	if m.focus < fieldCount {
		m.inputs[m.focus].Blur()
	}
	m.focus = (i + focusCount) % focusCount
	if m.focus == focusRemember {
		return nil
	}
	return m.inputs[m.focus].Focus()
}

func (m loginModel) credentials() (mailbox.Credentials, bool) {
	c := mailbox.Credentials{
		Host:     strings.TrimSpace(m.inputs[fieldHost].Value()),
		Port:     strings.TrimSpace(m.inputs[fieldPort].Value()),
		Username: strings.TrimSpace(m.inputs[fieldUser].Value()),
		Password: m.inputs[fieldPass].Value(),
	}
	ok := c.Host != "" && c.Port != "" && c.Username != "" && c.Password != ""
	return c, ok
}

func (m loginModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render("nmail — IMAP login") + "\n\n")
	for i, in := range m.inputs {
		cursor := "  "
		if i == m.focus {
			cursor = "> "
		}
		b.WriteString(cursor + labelStyle.Render(padRight(fieldLabels[i], 10)) + in.View() + "\n")
	}
	cursor, box := "  ", "[ ]"
	if m.focus == focusRemember {
		cursor = "> "
	}
	if m.remember {
		box = "[x]"
	}
	b.WriteString(cursor + labelStyle.Render(padRight("", 10)) + box + " Stay logged in\n")
	b.WriteString("\n" + helpStyle.Render("tab/↑↓ move • space toggle • enter next/submit • esc quit"))
	return b.String()
}

func padRight(s string, n int) string {
	if len(s) >= n {
		return s
	}
	return s + strings.Repeat(" ", n-len(s))
}
