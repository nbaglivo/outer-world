package main

import (
	"bytes"
	"errors"
	"io"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"nmail/internal/account"
	"nmail/internal/mailbox"
)

type fakeWindow struct {
	from, to time.Time
	msgs     []*mailbox.Message
	skipped  []error
	err      error
}

func (f *fakeWindow) Window(from, to time.Time) ([]*mailbox.Message, []error, error) {
	f.from, f.to = from, to
	return f.msgs, f.skipped, f.err
}

func TestBuildEditionWindow(t *testing.T) {
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	msg := func(id string, h int) *mailbox.Message {
		return &mailbox.Message{
			MessageID: id,
			Text:      "some words to read",
			Arrived:   time.Date(2026, 10, 15, h, 0, 0, 0, madrid),
		}
	}
	w := &fakeWindow{msgs: []*mailbox.Message{msg("a", 1), msg("b", 5)}}

	ed, err := buildEdition(w, time.Date(2026, 10, 16, 5, 50, 0, 0, madrid), madrid, io.Discard)
	if err != nil {
		t.Fatal(err)
	}
	if want := time.Date(2026, 10, 14, 6, 30, 0, 0, madrid); !w.from.Equal(want) {
		t.Errorf("from = %v, want %v", w.from, want)
	}
	if want := time.Date(2026, 10, 15, 6, 30, 0, 0, madrid); !w.to.Equal(want) {
		t.Errorf("to = %v, want %v", w.to, want)
	}
	if ed.Count() != 2 {
		t.Errorf("Count = %d, want 2", ed.Count())
	}
}

func TestBuildEditionError(t *testing.T) {
	boom := errors.New("boom")
	_, err := buildEdition(&fakeWindow{err: boom}, time.Now(), time.UTC, io.Discard)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
}

func TestBuildEditionWarnsAboutSkipped(t *testing.T) {
	w := &fakeWindow{
		msgs:    []*mailbox.Message{{MessageID: "a", Text: "words", Arrived: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}},
		skipped: []error{errors.New(`message 7 "broken": parse part: EOF`)},
	}
	var warn bytes.Buffer
	ed, err := buildEdition(w, time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), time.UTC, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sip: skipped message 7 \"broken\": parse part: EOF\n"; warn.String() != want {
		t.Errorf("warning = %q, want %q", warn.String(), want)
	}
	if ed.Count() != 1 {
		t.Errorf("Count = %d, want 1", ed.Count())
	}
}

func TestLogin(t *testing.T) {
	keyring.MockInit()
	store := account.Keyring{Service: "sip"}
	c := mailbox.Credentials{Host: "imap.example.com", Port: "993", Username: "subs"}

	if err := login(store, c, func() (string, error) { return "pw", nil }); err != nil {
		t.Fatal(err)
	}
	got, err := store.Load()
	want := []mailbox.Credentials{{Host: c.Host, Port: c.Port, Username: c.Username, Password: "pw"}}
	if err != nil || !slices.Equal(got, want) {
		t.Fatalf("got %+v err=%v", got, err)
	}
	if list, _ := (account.Keyring{}).Load(); len(list) != 0 {
		t.Fatalf("nmail sees sip's login: %+v", list)
	}
}

func TestLoginEmptyPassword(t *testing.T) {
	keyring.MockInit()
	store := account.Keyring{Service: "sip"}
	c := mailbox.Credentials{Host: "h", Port: "993", Username: "u"}

	err := login(store, c, func() (string, error) { return "", nil })
	if err == nil || !strings.Contains(err.Error(), "password") {
		t.Fatalf("err = %v, want a password error", err)
	}
	if list, _ := store.Load(); len(list) != 0 {
		t.Fatalf("saved %+v", list)
	}
}
