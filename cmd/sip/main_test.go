package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/zalando/go-keyring"

	"nmail/internal/account"
	"nmail/internal/mailbox"
	"nmail/internal/sip/mastodon"
	"nmail/internal/sip/piece"
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

	ed, err := buildEdition(w, nil, time.Date(2026, 10, 16, 5, 50, 0, 0, madrid), madrid, io.Discard)
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
	_, err := buildEdition(&fakeWindow{err: boom}, nil, time.Now(), time.UTC, io.Discard)
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
	ed, err := buildEdition(w, nil, time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC), time.UTC, &warn)
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

var oct16 = time.Date(2026, 10, 16, 8, 0, 0, 0, time.UTC)

func oneEmail() *fakeWindow {
	return &fakeWindow{msgs: []*mailbox.Message{{MessageID: "a", Text: "words", Arrived: time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)}}}
}

func fediPiece() piece.Piece {
	return piece.Piece{ID: "f1", Title: "toot", Text: "words", Words: 1, Channel: "fediverse", Arrived: time.Date(2026, 10, 15, 10, 0, 0, 0, time.UTC)}
}

func TestBuildEditionAddsFeed(t *testing.T) {
	var since, until time.Time
	f := feed{name: "mastodon", fetch: func(s, u time.Time, note func(string)) ([]piece.Piece, error) {
		since, until = s, u
		return []piece.Piece{fediPiece()}, nil
	}}
	var warn bytes.Buffer
	ed, err := buildEdition(oneEmail(), []feed{f}, oct16, time.UTC, &warn)
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	if !until.Equal(cutoff) || !since.Equal(time.Date(2026, 10, 15, 6, 30, 0, 0, time.UTC)) {
		t.Errorf("since=%v until=%v", since, until)
	}
	if ed.Count() != 2 || warn.Len() != 0 {
		t.Errorf("Count=%d warn=%q", ed.Count(), warn.String())
	}
}

func TestBuildEditionFeedError(t *testing.T) {
	f := feed{name: "mastodon", fetch: func(_, _ time.Time, _ func(string)) ([]piece.Piece, error) {
		return nil, errors.New("GET home: 404")
	}}
	var warn bytes.Buffer
	ed, err := buildEdition(oneEmail(), []feed{f}, oct16, time.UTC, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sip: mastodon: GET home: 404\n"; warn.String() != want {
		t.Errorf("warn = %q, want %q", warn.String(), want)
	}
	if ed.Count() != 1 {
		t.Errorf("Count = %d, want 1", ed.Count())
	}
}

func TestBuildEditionFeedSkipped(t *testing.T) {
	var warn bytes.Buffer
	ed, err := buildEdition(oneEmail(), []feed{{name: "mastodon"}}, oct16, time.UTC, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sip: mastodon skipped: run sip login mastodon\n"; warn.String() != want {
		t.Errorf("warn = %q, want %q", warn.String(), want)
	}
	if ed.Count() != 1 {
		t.Errorf("Count = %d, want 1", ed.Count())
	}
}

func TestBuildEditionFeedNote(t *testing.T) {
	f := feed{name: "mastodon", fetch: func(_, _ time.Time, note func(string)) ([]piece.Piece, error) {
		note("timeline only reaches back to X")
		return []piece.Piece{fediPiece()}, nil
	}}
	var warn bytes.Buffer
	ed, err := buildEdition(oneEmail(), []feed{f}, oct16, time.UTC, &warn)
	if err != nil {
		t.Fatal(err)
	}
	if want := "sip: mastodon: timeline only reaches back to X\n"; warn.String() != want {
		t.Errorf("warn = %q, want %q", warn.String(), want)
	}
	if ed.Count() != 2 {
		t.Errorf("Count = %d, want 2", ed.Count())
	}
}

func TestBuildEditionEmailErrorStillFails(t *testing.T) {
	boom := errors.New("boom")
	called := false
	f := feed{name: "mastodon", fetch: func(_, _ time.Time, _ func(string)) ([]piece.Piece, error) {
		called = true
		return nil, nil
	}}
	_, err := buildEdition(&fakeWindow{err: boom}, []feed{f}, oct16, time.UTC, io.Discard)
	if !errors.Is(err, boom) {
		t.Fatalf("err = %v, want it to wrap boom", err)
	}
	if called {
		t.Error("feed was fetched after an email error")
	}
}

func TestMastodonFeed(t *testing.T) {
	b, err := os.ReadFile("../../internal/sip/mastodon/testdata/home.json")
	if err != nil {
		t.Fatal(err)
	}
	var sts []mastodon.Status
	if err := json.Unmarshal(b, &sts); err != nil {
		t.Fatal(err)
	}
	madrid, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	fetch := func(tl mastodon.Timeline, herr error) ([]piece.Piece, []string, error) {
		f := mastodonFeed(func(context.Context, time.Time) (mastodon.Timeline, error) { return tl, herr }, madrid)
		var notes []string
		ps, err := f.fetch(time.Time{}, time.Time{}, func(s string) { notes = append(notes, s) })
		return ps, notes, err
	}

	ps, notes, err := fetch(mastodon.Timeline{Statuses: sts, Complete: true}, nil)
	if err != nil || len(ps) != 3 || len(notes) != 0 {
		t.Fatalf("complete: %d pieces, notes=%q, err=%v", len(ps), notes, err)
	}

	oldest := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	_, notes, err = fetch(mastodon.Timeline{Statuses: sts, Oldest: oldest}, nil)
	want := "timeline only reaches back to 2026-10-15T11:00:00+02:00"
	if err != nil || len(notes) != 1 || notes[0] != want {
		t.Fatalf("incomplete: notes=%q err=%v", notes, err)
	}

	boom := errors.New("boom")
	if _, _, err := fetch(mastodon.Timeline{}, boom); !errors.Is(err, boom) {
		t.Fatalf("err = %v", err)
	}
}

func TestLoginToken(t *testing.T) {
	keyring.MockInit()
	store := account.Secret{Service: "sip", Name: "mastodon-token"}
	if err := login(account.Keyring{Service: "sip"}, mailbox.Credentials{Host: "h", Port: "993", Username: "u"}, func() (string, error) { return "pw", nil }); err != nil {
		t.Fatal(err)
	}
	if err := loginToken(store, func() (string, error) { return "tok", nil }); err != nil {
		t.Fatal(err)
	}
	if got, err := store.Get(); err != nil || got != "tok" {
		t.Fatalf("got %q err=%v", got, err)
	}
	if list, _ := (account.Keyring{Service: "sip"}).Load(); len(list) != 1 {
		t.Fatalf("imap logins = %+v", list)
	}
}

func TestLoginTokenTrims(t *testing.T) {
	keyring.MockInit()
	store := account.Secret{Service: "sip", Name: "mastodon-token"}
	if err := loginToken(store, func() (string, error) { return "  tok\n", nil }); err != nil {
		t.Fatal(err)
	}
	if got, _ := store.Get(); got != "tok" {
		t.Fatalf("got %q", got)
	}
}

func TestLoginTokenEmpty(t *testing.T) {
	keyring.MockInit()
	store := account.Secret{Service: "sip", Name: "mastodon-token"}
	err := loginToken(store, func() (string, error) { return "  ", nil })
	if err == nil || !strings.Contains(err.Error(), "token") {
		t.Fatalf("err = %v", err)
	}
	if got, _ := store.Get(); got != "" {
		t.Fatalf("saved %q", got)
	}
}
