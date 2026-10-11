package render

import (
	"os"
	"regexp"
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"

	"nmail/internal/mailbox"
)

var reANSI = regexp.MustCompile(`\x1b\]8;;[^\x1b]*\x1b\\|\x1b\[[0-9;]*m`)

func plain(d Doc, width int) string {
	s, _ := d.Render(width, -1)
	return reANSI.ReplaceAllString(s, "")
}

const digest = `<html><head><style>p{color:red}</style><title>t</title></head><body>
<div style="display:none">preheader text</div>
<h1>Daily   Digest</h1>
<p>Three things <b>happened</b> today. Read <a href="https://example.com/story">the story</a>.</p>
<table><tr><td bgcolor="#3366ff"><a href="https://example.com/approve">Approve</a></td>
<td><a class="btn" href="https://example.com/reject">Reject</a></td></tr></table>
<a href="https://example.com/logo"><img src="logo.png" alt="Logo"></a>
<a href="https://example.com/pixel"><img src="p.gif"></a>
<a href="#top">top</a>
<ul><li>first item</li><li>second</li></ul>
<hr>
<p><a style="background-color:#000;padding:8px" href="mailto:me@example.com">Reply</a></p>
</body></html>`

func TestHTMLLinksAndButtons(t *testing.T) {
	d := HTML(digest)
	want := []Link{
		{URL: "https://example.com/story", Text: "the story"},
		{URL: "https://example.com/approve", Text: "Approve", Button: true, Color: "#3366ff"},
		{URL: "https://example.com/reject", Text: "Reject", Button: true},
		{URL: "https://example.com/logo", Text: "Logo"},
		{URL: "mailto:me@example.com", Text: "Reply", Button: true, Color: "#000000"},
	}
	if len(d.Links) != len(want) {
		t.Fatalf("links = %+v", d.Links)
	}
	for i := range want {
		if d.Links[i] != want[i] {
			t.Errorf("link %d = %+v, want %+v", i, d.Links[i], want[i])
		}
	}

	out := plain(d, 80)
	for _, s := range []string{"Daily Digest", "Three things happened today. Read the story.", " Approve ", " Reject ", "• first item", "────"} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
	for _, s := range []string{"preheader", "color:red", "p.gif"} {
		if strings.Contains(out, s) {
			t.Errorf("unexpected %q in:\n%s", s, out)
		}
	}
	if strings.Contains(out, "\n\n\n") {
		t.Errorf("more than one blank line in:\n%s", out)
	}
}

func TestWrapKeepsWidthAndButtons(t *testing.T) {
	d := HTML(`<p>` + strings.Repeat("word ", 30) + `<a class="button" href="https://x.io">Click this button</a></p><ul><li>` + strings.Repeat("long ", 20) + `</li></ul>`)
	out := plain(d, 30)
	for _, row := range strings.Split(out, "\n") {
		if w := lipgloss.Width(row); w > 30 {
			t.Errorf("row %q is %d wide", row, w)
		}
	}
	if !strings.Contains(out, " Click this button ") {
		t.Errorf("button broken across rows:\n%s", out)
	}
	if !strings.Contains(out, "\n    long") {
		t.Errorf("list continuation not hung under the bullet:\n%s", out)
	}
}

func TestRenderReportsLinkLines(t *testing.T) {
	d := HTML(`<p><a href="https://a.io">a</a></p><p>x</p><p><a href="https://b.io">b</a></p>`)
	s, starts := d.Render(40, 1)
	if len(starts) != 2 || starts[0] != 0 || starts[1] != 4 {
		t.Fatalf("starts = %v", starts)
	}
	if !strings.Contains(s, "\x1b]8;;https://b.io\x1b\\") {
		t.Errorf("no OSC 8 hyperlink in %q", s)
	}
}

func TestTextAutolinks(t *testing.T) {
	d := Text("see https://example.com/a?b=1.\r\n  mail mailto:x@y.z")
	if len(d.Links) != 2 || d.Links[0].URL != "https://example.com/a?b=1" || d.Links[1].URL != "mailto:x@y.z" {
		t.Fatalf("links = %+v", d.Links)
	}
	if out := plain(d, 80); out != "see https://example.com/a?b=1.\n  mail mailto:x@y.z" {
		t.Fatalf("got %q", out)
	}
}

func TestRules(t *testing.T) {
	msg := &mailbox.Message{
		Summary: mailbox.Summary{Subject: "Your Daily Digest — Oct 11"},
		Text:    "plain https://p.io",
		HTML:    `<a class="btn" href="https://h.io">Go</a>`,
	}
	d, name := Message(msg)
	if name != "daily digest" || len(d.Links) != 1 || !d.Links[0].Button {
		t.Fatalf("digest rendered as %q: %+v", name, d.Links)
	}
	msg.Subject = "Weekly news"
	d, name = Message(msg)
	if name != "" || d.Links[0].URL != "https://p.io" {
		t.Fatalf("default rendered as %q: %+v", name, d.Links)
	}
}

func TestDigest(t *testing.T) {
	src, err := os.ReadFile("testdata/digest.html")
	if err != nil {
		t.Fatal(err)
	}
	d, name := Message(&mailbox.Message{
		Summary: mailbox.Summary{Subject: "Daily digest 2026-10-10 - 2 picks"},
		HTML:    string(src),
	})
	if name != "daily digest" {
		t.Fatalf("rule = %q", name)
	}

	want := []Link{
		{Text: "👍 Liked this", Color: chipColor},
		{Text: "👎 Not this one", Color: chipColor},
		{Text: "👍 Liked this", Color: chipColor},
		{Text: "👎 Not this one", Color: chipColor},
		{Text: "Good selection", Color: "#2e7d32"},
		{Text: "Not my taste", Color: "#c62828"},
	}
	if len(d.Links) != len(want) {
		t.Fatalf("links = %+v", d.Links)
	}
	for i, w := range want {
		if l := d.Links[i]; l.Text != w.Text || l.Color != w.Color || !l.Button {
			t.Errorf("link %d = %+v, want a %s button %q", i, l, w.Color, w.Text)
		}
	}

	var cards, muted int
	for _, l := range d.Lines {
		if l.Bar == "#ff6d5a" {
			cards++
		}
		for _, s := range l.Spans {
			if s.Style == Muted && strings.HasPrefix(s.Text, "Why:") {
				muted++
			}
		}
	}
	if cards == 0 || muted != 2 {
		t.Errorf("%d card lines, %d muted Why lines", cards, muted)
	}

	out := plain(d, 72)
	if testing.Verbose() {
		t.Log("\n" + out)
	}
	for _, s := range []string{
		"▌ Confirm your subscription\n▌ Coffee Blog <noreply@example.com>\n▌\n▌ A subscription",
		" 👍 Liked this   👎 Not this one ",
		"Did I pick well today?\n\n Good selection   Not my taste \n\nYour rating",
	} {
		if !strings.Contains(out, s) {
			t.Errorf("missing %q in:\n%s", s, out)
		}
	}
}
