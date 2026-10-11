// Package ui is the Bubble Tea front end: login → inbox → reader, with any
// number of accounts connected side by side.
package ui

import (
	"fmt"
	"slices"
	"strings"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"

	"nmail/internal/mailbox"
)

const inboxLimit = 100

type screen int

const (
	screenLogin screen = iota
	screenInbox
	screenReader
)

var (
	titleStyle  = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	errStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("196"))
	helpStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
	labelStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("245"))
	activeStyle = lipgloss.NewStyle().Bold(true).Reverse(true)
)

// connectedMsg carries the result of a connect or refresh for one account.
type connectedMsg struct {
	creds   mailbox.Credentials
	session *mailbox.Session
	inbox   []mailbox.Summary
	err     error
}

type messageMsg struct {
	msg *mailbox.Message
	err error
}

type deletedMsg struct {
	creds   mailbox.Credentials
	uid     imap.UID
	trashed bool
	err     error
}

// Store keeps the remembered accounts between runs.
type Store interface {
	Load() ([]mailbox.Credentials, error)
	Save([]mailbox.Credentials) error
}

type account struct {
	creds      mailbox.Credentials
	remember   bool
	session    *mailbox.Session
	messages   []mailbox.Summary
	connecting bool
	err        error
}

func (acct *account) key() string { return accountKey(acct.creds) }

func accountKey(c mailbox.Credentials) string {
	return c.Username + "@" + c.Host + ":" + c.Port
}

type App struct {
	screen  screen
	width   int
	height  int
	loading bool
	err     error
	notice  string
	spinner spinner.Model

	store    Store
	defaults mailbox.Credentials
	accounts []*account
	current  int
	pending  *account         // login from the form awaiting its result
	doomed   *mailbox.Summary // message awaiting delete confirmation

	login  loginModel
	inbox  inboxModel
	reader readerModel
}

// New builds the app; Init connects every account saved in store.
func New(defaults mailbox.Credentials, store Store) *App {
	s := spinner.New()
	s.Spinner = spinner.Dot
	a := &App{
		spinner:  s,
		store:    store,
		defaults: defaults,
		login:    newLogin(defaults),
		inbox:    newInbox(),
		reader:   newReader(),
	}
	saved, err := store.Load()
	a.err = err
	for _, c := range saved {
		a.accounts = append(a.accounts, &account{creds: c, remember: true})
	}
	if len(a.accounts) > 0 {
		a.screen = screenInbox
	}
	return a
}

// Close logs out of every open IMAP session.
func (a *App) Close() {
	for _, acct := range a.accounts {
		if acct.session != nil {
			acct.session.Close()
		}
	}
}

func (a *App) Init() tea.Cmd {
	if len(a.accounts) == 0 {
		return a.login.Init()
	}
	cmds := []tea.Cmd{a.spinner.Tick}
	for _, acct := range a.accounts {
		acct.connecting = true
		cmds = append(cmds, connect(acct.creds))
	}
	return tea.Batch(cmds...)
}

func (a *App) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		a.width, a.height = msg.Width, msg.Height
		a.inbox.setSize(msg.Width, msg.Height)
		a.reader.setSize(msg.Width, msg.Height)
		return a, nil

	case tea.KeyMsg:
		if msg.String() == "ctrl+c" {
			return a, tea.Quit
		}
		if a.loading {
			return a, nil
		}
		a.notice = ""
		if a.doomed != nil {
			return a, a.confirmDelete(msg)
		}

	case spinner.TickMsg:
		if !a.busy() {
			return a, nil
		}
		var cmd tea.Cmd
		a.spinner, cmd = a.spinner.Update(msg)
		return a, cmd

	case connectedMsg:
		return a.connected(msg)

	case messageMsg:
		a.loading = false
		if msg.err != nil {
			a.err = msg.err
			return a, nil
		}
		a.err = nil
		a.reader.show(msg.msg)
		a.screen = screenReader
		return a, nil

	case deletedMsg:
		return a, a.deleted(msg)

	case openedMsg:
		a.err = msg.err
		return a, nil
	}

	switch a.screen {
	case screenLogin:
		return a.updateLogin(msg)
	case screenInbox:
		return a.updateInbox(msg)
	default:
		return a.updateReader(msg)
	}
}

// connected files a connect or refresh result under its account.
func (a *App) connected(msg connectedMsg) (tea.Model, tea.Cmd) {
	key := accountKey(msg.creds)
	if p := a.pending; p != nil && p.key() == key {
		a.pending = nil
		a.loading = false
		if msg.err != nil {
			a.err = msg.err
			return a, nil
		}
		p.session, p.messages = msg.session, msg.inbox
		var old *mailbox.Session
		i := a.indexOf(key)
		if i >= 0 {
			old = a.accounts[i].session
			a.accounts[i] = p
		} else {
			a.accounts = append(a.accounts, p)
			i = len(a.accounts) - 1
		}
		a.switchTo(i)
		a.persist()
		return a, closeSession(old)
	}

	i := a.indexOf(key)
	if i < 0 {
		// The account was logged out while this was in flight.
		return a, closeSession(msg.session)
	}
	acct := a.accounts[i]
	acct.connecting = false
	acct.err = msg.err
	var extra *mailbox.Session
	if msg.err != nil && msg.session != nil && msg.session == acct.session {
		// Refresh failed: drop the session so r reconnects.
		extra, acct.session = acct.session, nil
	}
	if msg.err == nil {
		acct.messages = msg.inbox
		if acct.session == nil {
			acct.session = msg.session
		} else if msg.session != acct.session {
			extra = msg.session
		}
	}
	if i == a.current {
		a.loading = false
		if a.screen == screenInbox {
			a.inbox.setMessages(acct.messages)
		}
	}
	return a, closeSession(extra)
}

func (a *App) updateLogin(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok && k.String() == "esc" {
		if len(a.accounts) == 0 {
			return a, tea.Quit
		}
		a.switchTo(a.current)
		return a, nil
	}
	creds, submit, cmd := a.login.Update(msg)
	if !submit {
		return a, cmd
	}
	a.loading = true
	a.err = nil
	a.pending = &account{creds: creds, remember: a.login.remember}
	return a, tea.Batch(a.spinner.Tick, connect(creds))
}

func (a *App) updateInbox(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "q", "esc":
			return a, tea.Quit
		case "tab":
			a.switchTo((a.current + 1) % len(a.accounts))
			return a, a.tickIfConnecting()
		case "shift+tab":
			a.switchTo((a.current + len(a.accounts) - 1) % len(a.accounts))
			return a, a.tickIfConnecting()
		case "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if i := int(k.Runes[0] - '1'); i < len(a.accounts) {
				a.switchTo(i)
				return a, a.tickIfConnecting()
			}
			return a, nil
		case "a":
			a.err = nil
			a.login = newLogin(a.defaults)
			a.screen = screenLogin
			return a, a.login.Init()
		case "L":
			return a, a.logout()
		case "r":
			acct := a.accounts[a.current]
			if acct.connecting {
				return a, nil
			}
			a.loading = true
			if acct.session == nil {
				acct.connecting = true
				return a, tea.Batch(a.spinner.Tick, connect(acct.creds))
			}
			return a, tea.Batch(a.spinner.Tick, refresh(acct))
		case "enter":
			acct := a.accounts[a.current]
			if s, ok := a.inbox.selected(); ok && acct.session != nil {
				a.loading = true
				return a, tea.Batch(a.spinner.Tick, read(acct.session, s.UID))
			}
			return a, nil
		case "d":
			if s, ok := a.inbox.selected(); ok && a.accounts[a.current].session != nil {
				a.doomed = &s
			}
			return a, nil
		}
	}
	var cmd tea.Cmd
	a.inbox, cmd = a.inbox.Update(msg)
	return a, cmd
}

func (a *App) updateReader(msg tea.Msg) (tea.Model, tea.Cmd) {
	if k, ok := msg.(tea.KeyMsg); ok {
		switch k.String() {
		case "esc":
			if !a.reader.blur() {
				a.screen = screenInbox
			}
			return a, nil
		case "q", "backspace":
			a.screen = screenInbox
			return a, nil
		case "tab":
			a.reader.step(1)
			return a, nil
		case "shift+tab":
			a.reader.step(-1)
			return a, nil
		case "enter":
			if l, ok := a.reader.focused(); ok {
				a.notice = "opening " + l.URL
				return a, open(l)
			}
			return a, nil
		case "d":
			if a.accounts[a.current].session != nil {
				s := a.reader.summary
				a.doomed = &s
			}
			return a, nil
		}
	}
	var cmd tea.Cmd
	a.reader, cmd = a.reader.Update(msg)
	return a, cmd
}

// confirmDelete deletes the doomed message on y and cancels on any other key.
func (a *App) confirmDelete(k tea.KeyMsg) tea.Cmd {
	s := *a.doomed
	a.doomed = nil
	acct := a.accounts[a.current]
	if k.String() != "y" || acct.session == nil {
		return nil
	}
	a.loading = true
	a.err = nil
	return tea.Batch(a.spinner.Tick, deleteMessage(acct, s.UID))
}

// deleted drops a deleted message from its account's inbox.
func (a *App) deleted(msg deletedMsg) tea.Cmd {
	i := a.indexOf(accountKey(msg.creds))
	if i == a.current {
		a.loading = false
	}
	if msg.err != nil {
		// Leave the inbox as is; r refreshes it if the delete half-happened.
		if i == a.current {
			a.err = msg.err
		}
		return nil
	}
	if i < 0 {
		return nil
	}
	acct := a.accounts[i]
	acct.messages = slices.DeleteFunc(slices.Clone(acct.messages), func(s mailbox.Summary) bool {
		return s.UID == msg.uid
	})
	if i != a.current {
		return nil
	}
	a.inbox.updateMessages(acct.messages)
	if a.screen == screenReader && a.reader.summary.UID == msg.uid {
		a.screen = screenInbox
	}
	if msg.trashed {
		a.notice = "moved to Trash"
	} else {
		a.notice = "deleted permanently"
	}
	return nil
}

func (a *App) switchTo(i int) {
	a.current = i
	a.err = nil
	a.inbox.setMessages(a.accounts[i].messages)
	a.screen = screenInbox
}

func (a *App) indexOf(key string) int {
	return slices.IndexFunc(a.accounts, func(acct *account) bool { return acct.key() == key })
}

func (a *App) busy() bool {
	if a.loading {
		return true
	}
	return len(a.accounts) > 0 && a.accounts[a.current].connecting
}

func (a *App) tickIfConnecting() tea.Cmd {
	if a.accounts[a.current].connecting {
		return a.spinner.Tick
	}
	return nil
}

// logout ends the current account's session and forgets it.
func (a *App) logout() tea.Cmd {
	acct := a.accounts[a.current]
	a.accounts = slices.Delete(a.accounts, a.current, a.current+1)
	if len(a.accounts) > 0 {
		a.switchTo(min(a.current, len(a.accounts)-1))
		a.persist()
		return closeSession(acct.session)
	}
	a.current = 0
	a.err = nil
	c := acct.creds
	c.Password = ""
	a.login = newLogin(c)
	a.screen = screenLogin
	a.persist()
	return tea.Batch(closeSession(acct.session), a.login.Init())
}

// persist saves every account marked to be remembered. It runs inline
// rather than as a command because quitting abandons pending commands.
func (a *App) persist() {
	var keep []mailbox.Credentials
	for _, acct := range a.accounts {
		if acct.remember {
			keep = append(keep, acct.creds)
		}
	}
	if err := a.store.Save(keep); err != nil {
		a.err = err
	}
}

// tabs renders the account switcher, or nothing for a single account.
func (a *App) tabs() string {
	if len(a.accounts) < 2 {
		return ""
	}
	parts := make([]string, len(a.accounts))
	for i, acct := range a.accounts {
		label := fmt.Sprintf(" %d %s", i+1, acct.creds.Username)
		if n := unread(acct.messages); n > 0 {
			label += fmt.Sprintf(" (%d)", n)
		}
		label += " "
		switch {
		case i == a.current:
			label = activeStyle.Render(label)
		case acct.err != nil:
			label = errStyle.Render(label)
		default:
			label = labelStyle.Render(label)
		}
		parts[i] = label
	}
	return strings.Join(parts, " ")
}

func unread(msgs []mailbox.Summary) int {
	n := 0
	for _, m := range msgs {
		if !m.Seen {
			n++
		}
	}
	return n
}

func (a *App) View() string {
	var body string
	switch a.screen {
	case screenLogin:
		body = a.login.View()
	case screenInbox:
		body = a.inbox.View(a.tabs())
	default:
		body = a.reader.View()
	}

	var acct *account
	if a.screen != screenLogin && len(a.accounts) > 0 {
		acct = a.accounts[a.current]
	}
	var status string
	switch {
	case a.doomed != nil:
		status = fmt.Sprintf("delete %q? (y/n)", a.doomed.Subject)
	case a.loading:
		status = a.spinner.View() + " working…"
	case acct != nil && acct.connecting:
		status = a.spinner.View() + " connecting " + acct.creds.Username + "…"
	case a.err != nil:
		status = errStyle.Render("error: " + a.err.Error())
	case acct != nil && acct.err != nil:
		status = errStyle.Render("error: " + acct.err.Error() + " (r to retry, L to log out)")
	case a.notice != "":
		status = labelStyle.Render(a.notice)
	}
	if status != "" {
		body += "\n" + status
	}
	return body
}

func connect(c mailbox.Credentials) tea.Cmd {
	return func() tea.Msg {
		s, err := mailbox.Connect(c)
		if err != nil {
			return connectedMsg{creds: c, err: err}
		}
		inbox, err := s.Inbox(inboxLimit)
		if err != nil {
			s.Close()
			return connectedMsg{creds: c, err: err}
		}
		return connectedMsg{creds: c, session: s, inbox: inbox}
	}
}

func refresh(acct *account) tea.Cmd {
	c, s := acct.creds, acct.session
	return func() tea.Msg {
		inbox, err := s.Inbox(inboxLimit)
		return connectedMsg{creds: c, session: s, inbox: inbox, err: err}
	}
}

func read(s *mailbox.Session, uid imap.UID) tea.Cmd {
	return func() tea.Msg {
		m, err := s.Read(uid)
		return messageMsg{msg: m, err: err}
	}
}

func deleteMessage(acct *account, uid imap.UID) tea.Cmd {
	c, s := acct.creds, acct.session
	return func() tea.Msg {
		trashed, err := s.Delete(uid)
		return deletedMsg{creds: c, uid: uid, trashed: trashed, err: err}
	}
}

func closeSession(s *mailbox.Session) tea.Cmd {
	if s == nil {
		return nil
	}
	return func() tea.Msg {
		s.Close()
		return nil
	}
}
