package mailbox

import (
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

const plainMsg = "From: Alice <alice@example.com>\r\n" +
	"To: bob@example.com\r\n" +
	"Subject: Hello\r\n" +
	"Date: Mon, 05 Oct 2026 10:00:00 +0000\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"Hi Bob!\r\n"

const htmlMsg = "From: carol@example.com\r\n" +
	"To: bob@example.com\r\n" +
	"Subject: Newsletter\r\n" +
	"Date: Tue, 06 Oct 2026 10:00:00 +0000\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=XX\r\n" +
	"\r\n" +
	"--XX\r\n" +
	"Content-Type: text/html; charset=iso-8859-1\r\n" +
	"\r\n" +
	"<html><head><style>p{}</style></head><body><p>Caf\xe9 &amp; more</p></body></html>\r\n" +
	"--XX--\r\n"

// newTestSession connects to an in-memory server holding plainMsg and htmlMsg
// in INBOX, plus any extra mailboxes.
func newTestSession(t *testing.T, extra map[string]*imap.CreateOptions) *Session {
	t.Helper()
	s := newEmptySession(t, extra)
	for _, raw := range []string{plainMsg, htmlMsg} {
		appendAt(t, s, raw, time.Time{})
	}
	return s
}

// newEmptySession is newTestSession without any messages.
func newEmptySession(t *testing.T, extra ...map[string]*imap.CreateOptions) *Session {
	t.Helper()

	mem := imapmemserver.New()
	user := imapmemserver.NewUser("bob", "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	for _, m := range extra {
		for name, opts := range m {
			if err := user.Create(name, opts); err != nil {
				t.Fatal(err)
			}
		}
	}
	mem.AddUser(user)

	srv := imapserver.New(&imapserver.Options{
		NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
			return mem.NewSession(), nil, nil
		},
		Caps:         imap.CapSet{imap.CapIMAP4rev1: {}, imap.CapIMAP4rev2: {}, imap.CapSpecialUse: {}},
		InsecureAuth: true,
	})
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })

	c, err := imapclient.DialInsecure(ln.Addr().String(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := c.Login("bob", "secret").Wait(); err != nil {
		t.Fatal(err)
	}
	s := &Session{client: c}
	t.Cleanup(s.Close)
	return s
}

// appendAt adds raw to INBOX with the given internal date (server's now if zero).
func appendAt(t *testing.T, s *Session, raw string, at time.Time) {
	t.Helper()
	var opts *imap.AppendOptions
	if !at.IsZero() {
		opts = &imap.AppendOptions{Time: at}
	}
	cmd := s.client.Append("INBOX", int64(len(raw)), opts)
	cmd.Write([]byte(raw))
	cmd.Close()
	if _, err := cmd.Wait(); err != nil {
		t.Fatal(err)
	}
}

func listMsg(subject string) string {
	return "From: Weekly <news@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: " + subject + "\r\n" +
		"Message-ID: <" + subject + "@example.com>\r\n" +
		"List-Id: Weekly <weekly.example.com>\r\n" +
		"List-Post: <https://example.com/p/1>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"body of " + subject + "\r\n"
}

func TestWindow(t *testing.T) {
	s := newEmptySession(t)
	day := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	appendAt(t, s, listMsg("before"), day.Add(-24*time.Hour-time.Minute))
	appendAt(t, s, listMsg("first"), day.Add(-24*time.Hour))
	appendAt(t, s, listMsg("second"), day.Add(-time.Minute))
	appendAt(t, s, listMsg("at-cutoff"), day)

	got, _, err := s.Window(day.Add(-24*time.Hour), day)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Subject != "first" || got[1].Subject != "second" {
		t.Fatalf("got %d messages, want first and second: %+v", len(got), got)
	}
	m := got[0]
	if !m.Arrived.Equal(day.Add(-24 * time.Hour)) {
		t.Errorf("Arrived = %v", m.Arrived)
	}
	if !got[1].Arrived.Equal(day.Add(-time.Minute)) {
		t.Errorf("second Arrived = %v", got[1].Arrived)
	}
	if m.ListID != "Weekly <weekly.example.com>" || m.ListPost != "<https://example.com/p/1>" {
		t.Errorf("ListID=%q ListPost=%q", m.ListID, m.ListPost)
	}
	if m.MessageID != "first@example.com" || m.FromAddr != "news@example.com" {
		t.Errorf("MessageID=%q FromAddr=%q", m.MessageID, m.FromAddr)
	}
	if len(m.Raw) == 0 {
		t.Error("Raw is empty")
	}
}

func TestWindowEmpty(t *testing.T) {
	s := newEmptySession(t)
	day := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	got, _, err := s.Window(day.Add(-24*time.Hour), day)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
	appendAt(t, s, listMsg("old"), day.Add(-72*time.Hour))
	got, _, err = s.Window(day.Add(-24*time.Hour), day)
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, %v; want none", got, err)
	}
}

func TestWindowIsReadOnly(t *testing.T) {
	s := newEmptySession(t)
	day := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	appendAt(t, s, listMsg("a"), day.Add(-time.Hour))
	appendAt(t, s, listMsg("b"), day.Add(-2*time.Hour))
	if got, _, err := s.Window(day.Add(-24*time.Hour), day); err != nil || len(got) != 2 {
		t.Fatalf("got %v, %v", got, err)
	}
	inbox, err := s.Inbox(10)
	if err != nil || len(inbox) != 2 {
		t.Fatalf("got %v, %v", inbox, err)
	}
	for _, m := range inbox {
		if m.Seen {
			t.Errorf("%q was marked seen", m.Subject)
		}
	}
}

func TestInboxAndRead(t *testing.T) {
	s := newTestSession(t, nil)

	inbox, err := s.Inbox(10)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 2 {
		t.Fatalf("got %d messages, want 2", len(inbox))
	}
	if inbox[0].Subject != "Newsletter" || inbox[1].From != "Alice" {
		t.Fatalf("unexpected order/content: %+v", inbox)
	}

	plain, err := s.Read(inbox[1].UID)
	if err != nil {
		t.Fatal(err)
	}
	if strings.TrimSpace(plain.Body) != "Hi Bob!" || plain.To != "bob@example.com" {
		t.Fatalf("plain message: %+v", plain)
	}
	if string(plain.Raw) != plainMsg {
		t.Fatalf("raw message = %q", plain.Raw)
	}

	html, err := s.Read(inbox[0].UID)
	if err != nil {
		t.Fatal(err)
	}
	if html.Body != "Café & more" {
		t.Fatalf("html body = %q", html.Body)
	}
}

func TestInboxLimit(t *testing.T) {
	s := newTestSession(t, nil)
	inbox, err := s.Inbox(1)
	if err != nil {
		t.Fatal(err)
	}
	if len(inbox) != 1 || inbox[0].Subject != "Newsletter" {
		t.Fatalf("got %+v, want only the newest message", inbox)
	}
}

func TestPickTrash(t *testing.T) {
	trash := []imap.MailboxAttr{imap.MailboxAttrTrash}
	for _, tc := range []struct {
		list []*imap.ListData
		want string
	}{
		{[]*imap.ListData{{Mailbox: "INBOX"}}, ""},
		{[]*imap.ListData{{Mailbox: "INBOX"}, {Mailbox: "trash"}}, "trash"},
		{[]*imap.ListData{{Mailbox: "Trash"}, {Mailbox: "[Gmail]/Bin", Attrs: trash}}, "[Gmail]/Bin"},
	} {
		if got := pickTrash(tc.list); got != tc.want {
			t.Errorf("pickTrash(%v) = %q, want %q", tc.list, got, tc.want)
		}
	}
}

func TestDeleteFallsBackToTrashByName(t *testing.T) {
	s := newTestSession(t, map[string]*imap.CreateOptions{"Trash": nil})
	inbox, _ := s.Inbox(10)
	if trashed, err := s.Delete(inbox[1].UID); err != nil || !trashed {
		t.Fatalf("trashed=%v err=%v", trashed, err)
	}
	assertInbox(t, s, "Newsletter")

	sel, err := s.client.Select("Trash", nil).Wait()
	if err != nil {
		t.Fatal(err)
	}
	if sel.NumMessages != 1 {
		t.Fatalf("Trash holds %d messages, want 1", sel.NumMessages)
	}
}

func TestDeleteWithoutTrashExpunges(t *testing.T) {
	s := newTestSession(t, nil)
	inbox, _ := s.Inbox(10)
	trashed, err := s.Delete(inbox[1].UID)
	if err != nil {
		t.Fatal(err)
	}
	if trashed {
		t.Fatal("reported trashed with no Trash mailbox")
	}
	assertInbox(t, s, "Newsletter")
}

func assertInbox(t *testing.T, s *Session, subjects ...string) {
	t.Helper()
	inbox, err := s.Inbox(10)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range inbox {
		got = append(got, m.Subject)
	}
	if !slices.Equal(got, subjects) {
		t.Fatalf("INBOX = %q, want %q", got, subjects)
	}
}

func TestParse(t *testing.T) {
	raw := "From: \"Robin R\" <robin@example.com>\r\n" +
		"To: bob@example.com\r\n" +
		"Subject: Hello\r\n" +
		"Date: Mon, 05 Oct 2026 10:00:00 +0000\r\n" +
		"Message-ID: <abc@example.com>\r\n" +
		"List-Id: \"Money Stuff\" <money.example.com>\r\n" +
		"List-Post: <https://example.com/p/9>\r\n" +
		"Content-Type: text/plain; charset=utf-8\r\n" +
		"\r\n" +
		"Hi Bob!\r\n"
	m, err := Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "Hello" || m.From != "Robin R" || m.FromAddr != "robin@example.com" || m.To != "bob@example.com" {
		t.Errorf("envelope = %q %q %q %q", m.Subject, m.From, m.FromAddr, m.To)
	}
	if want := time.Date(2026, 10, 5, 10, 0, 0, 0, time.UTC); !m.Date.Equal(want) {
		t.Errorf("Date = %v", m.Date)
	}
	if m.MessageID != "abc@example.com" {
		t.Errorf("MessageID = %q", m.MessageID)
	}
	if m.ListID != `"Money Stuff" <money.example.com>` || m.ListPost != "<https://example.com/p/9>" {
		t.Errorf("list = %q %q", m.ListID, m.ListPost)
	}
	if m.Text != "Hi Bob!\r\n" || m.Body != m.Text || string(m.Raw) != raw {
		t.Errorf("body = %q %q", m.Text, m.Body)
	}
	if m.UID != 0 || !m.Arrived.IsZero() {
		t.Errorf("UID/Arrived = %d %v", m.UID, m.Arrived)
	}
}

// SEARCH SINCE compares dates in the server's zone, which can be a day behind
// a from given in a far-east zone.
func TestWindowFromAheadOfServerDate(t *testing.T) {
	s := newEmptySession(t)
	east, err := time.LoadLocation("Etc/GMT-12")
	if err != nil {
		t.Fatal(err)
	}
	from := time.Date(2026, 10, 16, 6, 30, 0, 0, east)
	appendAt(t, s, listMsg("early"), from.Add(time.Minute).UTC())

	got, _, err := s.Window(from, from.Add(24*time.Hour))
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Subject != "early" {
		t.Fatalf("got %d messages, want early: %+v", len(got), got)
	}
}

const unknownCharsetMsg = "From: news@example.com\r\n" +
	"Subject: odd charset\r\n" +
	"Content-Type: text/plain; charset=x-unknown-zz\r\n" +
	"\r\n" +
	"raw words\r\n"

const brokenMultipartMsg = "From: news@example.com\r\n" +
	"Subject: broken\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=XX\r\n" +
	"\r\n" +
	"--XX\r\n" +
	"Content-Type: text/plain\r\n" +
	"\r\n" +
	"never ends\r\n"

func TestWindowSkipsUnparsable(t *testing.T) {
	s := newEmptySession(t)
	day := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	appendAt(t, s, listMsg("good"), day.Add(-3*time.Hour))
	appendAt(t, s, unknownCharsetMsg, day.Add(-2*time.Hour))
	appendAt(t, s, brokenMultipartMsg, day.Add(-90*time.Minute))
	appendAt(t, s, listMsg("also-good"), day.Add(-time.Hour))

	got, skipped, err := s.Window(day.Add(-24*time.Hour), day)
	if err != nil {
		t.Fatal(err)
	}
	var subjects []string
	for _, m := range got {
		subjects = append(subjects, m.Subject)
	}
	if want := []string{"good", "odd charset", "also-good"}; !slices.Equal(subjects, want) {
		t.Fatalf("subjects = %q, want %q", subjects, want)
	}
	if got[1].Text != "raw words\r\n" {
		t.Errorf("unknown-charset Text = %q, want the raw bytes", got[1].Text)
	}
	if len(skipped) != 1 || !strings.Contains(skipped[0].Error(), "broken") {
		t.Errorf("skipped = %v, want the broken message", skipped)
	}
}

func TestParseUnknownCharset(t *testing.T) {
	m, err := Parse([]byte(unknownCharsetMsg))
	if err != nil {
		t.Fatal(err)
	}
	if m.Subject != "odd charset" || m.Text != "raw words\r\n" {
		t.Errorf("Subject=%q Text=%q", m.Subject, m.Text)
	}
}
