package mailbox_test

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"nmail/internal/mailbox"
	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
	"nmail/internal/sip/web"
)

var newsletter = "From: Robin Sloan <robin@example.com>\r\n" +
	"To: me@example.com\r\n" +
	"Subject: [Weekly] Notes on slow software\r\n" +
	"Date: Thu, 15 Oct 2026 09:15:00 +0200\r\n" +
	"Message-ID: <slow-1@example.com>\r\n" +
	"List-Id: =?utf-8?Q?Year_of_the_Meteor?= <meteor.example.com>\r\n" +
	"List-Post: <mailto:robin@example.com>, <https://example.com/p/slow>\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=XX\r\n" +
	"\r\n" +
	"--XX\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"View this email in your browser (https://example.com/web/slow)\r\n" +
	"\r\n" +
	"Some tools refuse to hurry you.\r\n" +
	"--XX\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<html><head><style>p{color:red}</style></head><body>" +
	"<div style=\"display:none;max-height:0\">On tools that refuse to hurry you&#8202;&#847;&zwnj;&nbsp;&#847;&zwnj;&nbsp;&#65279;</div>" +
	"<table><tr><td><a href=\"https://example.com/web/slow\">View this email in your browser</a></td></tr></table>" +
	"<h1>Notes on slow software</h1>" +
	strings.Repeat("<p>Some tools refuse to hurry you &mdash; and that&#8217;s the point of them, really.</p>", 80) +
	"<img src=\"https://t.example.com/open.gif\" style=\"width:1px;height:1px\">" +
	"<p><a href=\"https://example.com/web/slow\">View online</a></p>" +
	"</body></html>\r\n" +
	"--XX--\r\n"

const short = "From: Go Team <golang-announce@example.com>\r\n" +
	"Subject: Go 1.27.3 is released\r\n" +
	"MIME-Version: 1.0\r\n" +
	"Content-Type: multipart/alternative; boundary=YY\r\n" +
	"\r\n" +
	"--YY\r\n" +
	"Content-Type: text/plain; charset=utf-8\r\n" +
	"\r\n" +
	"View this email in your browser\r\n\r\nGo 1.27.3 is out with a fix.\r\n" +
	"--YY\r\n" +
	"Content-Type: text/html; charset=utf-8\r\n" +
	"\r\n" +
	"<p><a href=\"https://example.com/web/go\">View in browser</a></p><p>Go 1.27.3 is out with a fix for the race detector on arm64.</p>\r\n" +
	"--YY--\r\n"

// TestEditionFromIMAP runs one newsletter from the server to the pages.
func TestEditionFromIMAP(t *testing.T) {
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, loc)
	s := mailbox.NewEmptySession(t)
	mailbox.AppendAt(t, s, newsletter, cutoff.Add(-21*time.Hour))
	mailbox.AppendAt(t, s, short, cutoff.Add(-10*time.Hour))

	msgs, skipped, err := s.Window(edition.Start(cutoff), cutoff)
	if err != nil || len(skipped) != 0 || len(msgs) != 2 {
		t.Fatalf("Window = %d messages, skipped %v, err %v", len(msgs), skipped, err)
	}
	var pieces []piece.Piece
	for _, m := range msgs {
		pieces = append(pieces, piece.FromEmail(m))
	}
	h := web.Handler(edition.Compose(pieces, cutoff), loc)

	get := func(path string) string {
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d", path, rec.Code)
		}
		return rec.Body.String()
	}
	chrome := []string{"View", "browser", "open.gif", "&amp;", "\u034f", "\u200c", "\ufeff", "Robin", "Meteor"}

	front := get("/")
	for _, want := range []string{
		`<a href="/p/1">Notes on slow software</a>`,
		`<p class="dek">On tools that refuse to hurry you <span class="mono">`,
		`<a class="brief" href="/p/2">Go 1.27.3 is out with a fix for the race detector on arm64.</a>`,
	} {
		if !strings.Contains(front, want) {
			t.Errorf("front page lacks %q:\n%s", want, front)
		}
	}
	for _, bad := range chrome {
		if strings.Contains(front, bad) {
			t.Errorf("front page contains %q", bad)
		}
	}

	page := get("/p/1")
	for _, want := range []string{"<h1>Notes on slow software</h1>", "that’s the point", "Year of the Meteor", `href="https://example.com/p/slow"`} {
		if !strings.Contains(page, want) {
			t.Errorf("/p/1 lacks %q", want)
		}
	}
	for _, bad := range chrome[:5] {
		if strings.Contains(page, bad) {
			t.Errorf("/p/1 contains %q", bad)
		}
	}
}
