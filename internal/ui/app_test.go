package ui

import (
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"nmail/internal/mailbox"
)

func typeText(m *loginModel, s string) {
	for _, r := range s {
		m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
}

func TestLoginSubmit(t *testing.T) {
	m := newLogin(mailbox.Credentials{Port: "993"})
	enter := tea.KeyMsg{Type: tea.KeyEnter}

	typeText(&m, "imap.example.com")
	m.Update(enter)
	m.Update(enter) // keep default port
	typeText(&m, "me@example.com")
	m.Update(enter)

	if _, ok, _ := m.Update(enter); ok {
		t.Fatal("submitted without a password")
	}
	typeText(&m, "pw")
	creds, ok, _ := m.Update(enter)
	if !ok {
		t.Fatal("form not submitted")
	}
	want := mailbox.Credentials{Host: "imap.example.com", Port: "993", Username: "me@example.com", Password: "pw"}
	if creds != want {
		t.Fatalf("got %+v, want %+v", creds, want)
	}
	if strings.Contains(m.View(), "pw\n") {
		t.Fatal("password rendered in clear text")
	}
}

func TestInboxAndReaderRender(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{saved: []mailbox.Credentials{alice}})
	a.Init()
	a.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	a.Update(connectedMsg{creds: alice, inbox: []mailbox.Summary{
		{UID: 2, From: "Alice", Subject: "Hello there", Date: time.Now()},
		{UID: 1, From: "Carol", Subject: "Old news", Seen: true},
	}})

	v := a.View()
	for _, s := range []string{"INBOX — 2 messages", "Alice", "Hello there", "Old news"} {
		if !strings.Contains(v, s) {
			t.Fatalf("inbox view missing %q:\n%s", s, v)
		}
	}

	a.Update(messageMsg{msg: &mailbox.Message{
		Summary: mailbox.Summary{UID: 2, From: "Alice", Subject: "Hello there"},
		To:      "me",
		Body:    "line one\r\nline two",
	}})
	v = a.View()
	if a.screen != screenReader || !strings.Contains(v, "line two") {
		t.Fatalf("reader view:\n%s", v)
	}

	a.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if a.screen != screenInbox {
		t.Fatal("esc did not return to inbox")
	}
}

func TestLoginDefaultsFocusFirstEmptyField(t *testing.T) {
	m := newLogin(mailbox.Credentials{Host: "imap.migadu.com", Port: "993", Username: "me@example.com"})
	if m.focus != fieldPass {
		t.Fatalf("focus = %d, want password field", m.focus)
	}
	typeText(&m, "pw")
	creds, ok, _ := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	if !ok || creds.Host != "imap.migadu.com" || creds.Username != "me@example.com" {
		t.Fatalf("got %+v, submitted=%v", creds, ok)
	}
}

var (
	errTest = errors.New("test")
	alice   = mailbox.Credentials{Host: "h", Port: "993", Username: "alice", Password: "pw"}
	bob     = mailbox.Credentials{Host: "h2", Port: "993", Username: "bob", Password: "pw2"}
)

type fakeStore struct{ saved []mailbox.Credentials }

func (f *fakeStore) Load() ([]mailbox.Credentials, error) { return f.saved, nil }
func (f *fakeStore) Save(l []mailbox.Credentials) error   { f.saved = l; return nil }

// run executes cmd, descending into batches.
func run(a *App, cmd tea.Cmd) {
	if cmd == nil {
		return
	}
	if msg, ok := cmd().(tea.BatchMsg); ok {
		for _, c := range msg {
			run(a, c)
		}
	}
}

func key(s string) tea.KeyMsg {
	switch s {
	case "enter":
		return tea.KeyMsg{Type: tea.KeyEnter}
	case "esc":
		return tea.KeyMsg{Type: tea.KeyEsc}
	case "tab":
		return tea.KeyMsg{Type: tea.KeyTab}
	case " ":
		return tea.KeyMsg{Type: tea.KeySpace, Runes: []rune{' '}}
	}
	return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)}
}

// addAccount logs in through the form and delivers a successful connection.
func addAccount(a *App, c mailbox.Credentials, inbox []mailbox.Summary) {
	if a.screen != screenLogin {
		run(a, func() tea.Msg { _, cmd := a.Update(key("a")); return cmd })
	}
	a.login = newLogin(c)
	a.Update(key("enter"))
	_, cmd := a.Update(connectedMsg{creds: c, inbox: inbox})
	run(a, cmd)
}

func TestSavedAccountsConnectOnStart(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{saved: []mailbox.Credentials{alice, bob}})
	a.Init()
	if a.screen != screenInbox || len(a.accounts) != 2 || !a.accounts[0].connecting || !a.accounts[1].connecting {
		t.Fatalf("saved accounts not connecting: screen=%d", a.screen)
	}
	a.Update(connectedMsg{creds: bob, inbox: []mailbox.Summary{{UID: 1, Subject: "for bob"}}})
	if strings.Contains(a.View(), "for bob") {
		t.Fatal("background account leaked into current inbox")
	}
	a.Update(connectedMsg{creds: alice, err: errTest})
	if v := a.View(); !strings.Contains(v, "r to retry") {
		t.Fatalf("connect error not shown:\n%s", v)
	}
	a.Update(key("tab"))
	if v := a.View(); !strings.Contains(v, "for bob") {
		t.Fatalf("switch did not show bob's inbox:\n%s", v)
	}
}

func TestAddSwitchAndLogout(t *testing.T) {
	store := &fakeStore{}
	a := New(mailbox.Credentials{}, store)
	addAccount(a, alice, []mailbox.Summary{{UID: 1, Subject: "for alice"}})
	addAccount(a, bob, []mailbox.Summary{{UID: 1, Subject: "for bob"}})

	if len(a.accounts) != 2 || a.current != 1 || len(store.saved) != 2 {
		t.Fatalf("accounts=%d current=%d saved=%d", len(a.accounts), a.current, len(store.saved))
	}
	v := a.View()
	for _, s := range []string{"for bob", "1 alice", "2 bob"} {
		if !strings.Contains(v, s) {
			t.Fatalf("view missing %q:\n%s", s, v)
		}
	}

	a.Update(key("1"))
	if !strings.Contains(a.View(), "for alice") {
		t.Fatal("1 did not switch to alice")
	}

	_, cmd := a.Update(key("L"))
	run(a, cmd)
	if len(a.accounts) != 1 || a.screen != screenInbox || !strings.Contains(a.View(), "for bob") {
		t.Fatalf("logout of alice: accounts=%d\n%s", len(a.accounts), a.View())
	}
	if len(store.saved) != 1 || store.saved[0] != bob {
		t.Fatalf("saved after logout: %+v", store.saved)
	}

	_, cmd = a.Update(key("L"))
	run(a, cmd)
	if a.screen != screenLogin || len(store.saved) != 0 {
		t.Fatalf("last logout: screen=%d saved=%d", a.screen, len(store.saved))
	}
	if c, _ := a.login.credentials(); c.Host != "h2" || c.Username != "bob" || c.Password != "" {
		t.Fatalf("login form after logout: %+v", c)
	}
}

func TestEscFromAddAccountReturnsToInbox(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{})
	addAccount(a, alice, nil)
	a.Update(key("a"))
	if a.screen != screenLogin {
		t.Fatal("a did not open the login form")
	}
	a.Update(key("esc"))
	if a.screen != screenInbox {
		t.Fatal("esc did not return to inbox")
	}
}

func TestFailedAddKeepsOtherAccounts(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{})
	addAccount(a, alice, nil)
	a.Update(key("a"))
	a.login = newLogin(bob)
	a.Update(key("enter"))
	a.Update(connectedMsg{creds: bob, err: errTest})
	if a.screen != screenLogin || len(a.accounts) != 1 || a.err == nil {
		t.Fatalf("screen=%d accounts=%d err=%v", a.screen, len(a.accounts), a.err)
	}
}

func TestUncheckedRememberIsNotSaved(t *testing.T) {
	store := &fakeStore{}
	a := New(mailbox.Credentials{}, store)
	a.login = newLogin(alice)
	a.login.setFocus(focusRemember)
	a.Update(key(" "))
	if a.login.remember {
		t.Fatal("space did not toggle remember")
	}
	a.Update(key("enter"))
	_, cmd := a.Update(connectedMsg{creds: alice})
	run(a, cmd)
	if len(a.accounts) != 1 || len(store.saved) != 0 {
		t.Fatalf("accounts=%d saved=%d", len(a.accounts), len(store.saved))
	}
}

// Quitting doesn't wait for pending commands, so saves must land in Update.
func TestLoginAndLogoutSaveBeforeReturning(t *testing.T) {
	store := &fakeStore{}
	a := New(mailbox.Credentials{}, store)
	a.login = newLogin(alice)
	a.Update(key("enter"))
	a.Update(connectedMsg{creds: alice})
	if len(store.saved) != 1 {
		t.Fatalf("login not saved before quit: %+v", store.saved)
	}
	a.Update(key("L"))
	if len(store.saved) != 0 {
		t.Fatalf("logout not saved before quit: %+v", store.saved)
	}
}

func TestDeleteFromInbox(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{})
	addAccount(a, alice, []mailbox.Summary{
		{UID: 3, Subject: "three"}, {UID: 2, Subject: "two"}, {UID: 1, Subject: "one"},
	})
	a.accounts[0].session = &mailbox.Session{}
	a.Update(key("j"))

	a.Update(key("d"))
	if v := a.View(); !strings.Contains(v, `delete "two"? (y/n)`) {
		t.Fatalf("no confirmation prompt:\n%s", v)
	}
	if _, cmd := a.Update(key("n")); cmd != nil || a.loading {
		t.Fatal("n did not cancel the delete")
	}

	a.Update(key("d"))
	if _, cmd := a.Update(key("y")); cmd == nil || !a.loading {
		t.Fatal("y did not start the delete")
	}
	a.Update(deletedMsg{creds: alice, uid: 2, trashed: true})
	v := a.View()
	if strings.Contains(v, "two") || !strings.Contains(v, "moved to Trash") || len(a.accounts[0].messages) != 2 {
		t.Fatalf("message not removed:\n%s", v)
	}
	if s, _ := a.inbox.selected(); s.UID != 1 {
		t.Fatalf("cursor on %d, want the next message", s.UID)
	}
}

func TestDeleteFromReaderReturnsToInbox(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{})
	addAccount(a, alice, []mailbox.Summary{{UID: 1, Subject: "only"}})
	a.accounts[0].session = &mailbox.Session{}
	a.Update(messageMsg{msg: &mailbox.Message{Summary: mailbox.Summary{UID: 1, Subject: "only"}}})

	a.Update(key("d"))
	a.Update(key("y"))
	a.Update(deletedMsg{creds: alice, uid: 1})
	if a.screen != screenInbox || len(a.accounts[0].messages) != 0 || !strings.Contains(a.View(), "deleted permanently") {
		t.Fatalf("screen=%d messages=%d\n%s", a.screen, len(a.accounts[0].messages), a.View())
	}
}

func TestDeleteErrorKeepsMessage(t *testing.T) {
	a := New(mailbox.Credentials{}, &fakeStore{})
	addAccount(a, alice, []mailbox.Summary{{UID: 1, Subject: "only"}})
	a.accounts[0].session = &mailbox.Session{}
	a.Update(key("d"))
	a.Update(key("y"))
	a.Update(deletedMsg{creds: alice, uid: 1, err: errTest})
	if len(a.accounts[0].messages) != 1 || a.err == nil || a.loading {
		t.Fatalf("messages=%d err=%v loading=%v", len(a.accounts[0].messages), a.err, a.loading)
	}
}
