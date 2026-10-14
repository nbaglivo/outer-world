package mailbox

import (
	"net"
	"slices"
	"strings"
	"testing"

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

	mem := imapmemserver.New()
	user := imapmemserver.NewUser("bob", "secret")
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	for name, opts := range extra {
		if err := user.Create(name, opts); err != nil {
			t.Fatal(err)
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
	for _, raw := range []string{plainMsg, htmlMsg} {
		cmd := c.Append("INBOX", int64(len(raw)), nil)
		cmd.Write([]byte(raw))
		cmd.Close()
		if _, err := cmd.Wait(); err != nil {
			t.Fatal(err)
		}
	}
	s := &Session{client: c}
	t.Cleanup(s.Close)
	return s
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
