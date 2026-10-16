package piece

import (
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"nmail/internal/mailbox"
)

var arrived = time.Date(2026, 10, 9, 6, 0, 0, 0, time.UTC)

func parse(t *testing.T, raw string) *mailbox.Message {
	t.Helper()
	m, err := mailbox.Parse([]byte(raw))
	if err != nil {
		t.Fatal(err)
	}
	m.Arrived = arrived
	return m
}

func fixture(t *testing.T, name string) *mailbox.Message {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return parse(t, string(raw))
}

func TestFromEmailPlain(t *testing.T) {
	p := FromEmail(fixture(t, "plain.eml"))
	if p.ID != "plain-1@example.com" || p.Channel != "email" || !p.Arrived.Equal(arrived) {
		t.Errorf("ID/Channel/Arrived = %q %q %v", p.ID, p.Channel, p.Arrived)
	}
	if p.Title != "Notes on slow software" {
		t.Errorf("Title = %q", p.Title)
	}
	if p.Author != "Robin" || p.Source != "Robin" {
		t.Errorf("Author/Source = %q %q", p.Author, p.Source)
	}
	if p.Minutes() != 4 {
		t.Errorf("Minutes = %d (Words %d)", p.Minutes(), p.Words)
	}
	if !strings.HasPrefix(string(p.HTML), "<p>") {
		t.Errorf("HTML = %.40s", p.HTML)
	}
	if p.Dek != "Slow software is a choice we keep making." {
		t.Errorf("Dek = %q", p.Dek)
	}
	if want := time.Date(2026, 10, 9, 7, 0, 0, 0, time.UTC); !p.Published.Equal(want) {
		t.Errorf("Published = %v", p.Published)
	}
}

func TestFromEmailHTMLPreheader(t *testing.T) {
	p := FromEmail(fixture(t, "html-preheader.eml"))
	if p.Author != "news@letters.example.org" || p.Source != "letters.example.org" {
		t.Errorf("Author/Source = %q %q", p.Author, p.Source)
	}
	if p.Dek != "Rates, again, but calmly." {
		t.Errorf("Dek = %q", p.Dek)
	}
	if strings.Contains(string(p.HTML), "pixel.gif") {
		t.Errorf("HTML has pixel: %s", p.HTML)
	}
	if p.Text == "" {
		t.Error("Text is empty")
	}
}

func TestFromEmailListID(t *testing.T) {
	p := FromEmail(fixture(t, "list-id.eml"))
	if p.Source != "Money Stuff" {
		t.Errorf("Source = %q", p.Source)
	}
	if p.Original != "https://example.com/p/9" {
		t.Errorf("Original = %q", p.Original)
	}
	if !p.Published.Equal(arrived) {
		t.Errorf("Published = %v, want arrived %v", p.Published, arrived)
	}
}

func TestFromEmailDigest(t *testing.T) {
	body, err := os.ReadFile("../../render/testdata/digest.html")
	if err != nil {
		t.Fatal(err)
	}
	raw := "From: Digest <digest@example.com>\r\nSubject: Daily digest 2026-10-10 - 2 picks\r\n" +
		"Content-Type: text/html; charset=utf-8\r\n\r\n" + string(body)
	m := parse(t, raw)
	p := FromEmail(m)
	if p.Title != m.Subject || p.Title == "" {
		t.Errorf("Title = %q, Subject = %q", p.Title, m.Subject)
	}
	if p.Words == 0 {
		t.Error("Words = 0")
	}
	if !strings.Contains(string(p.HTML), "Confirm your subscription") {
		t.Errorf("HTML lacks digest text: %s", p.HTML)
	}
	if p.ID != "uid:0" {
		t.Errorf("ID = %q", p.ID)
	}
}

func TestFromEmailEmptyTitleHeadlineIsDek(t *testing.T) {
	for _, subject := range []string{"[Weekly]", ""} {
		m := parse(t, "From: a@example.com\r\nSubject: "+subject+"\r\nContent-Type: text/plain\r\n\r\nHello there. More.\r\n")
		p := FromEmail(m)
		if p.Title != "" {
			t.Errorf("subject %q: Title = %q", subject, p.Title)
		}
		if p.Dek != "Hello there." || p.Headline() != p.Dek {
			t.Errorf("subject %q: Dek = %q, Headline = %q", subject, p.Dek, p.Headline())
		}
	}
}

func TestFromEmailLongDekIsCut(t *testing.T) {
	long := strings.Repeat("word ", 60) + "end."
	p := FromEmail(parse(t, "From: a@example.com\r\nSubject: x\r\nContent-Type: text/plain\r\n\r\n"+long+" Next.\r\n"))
	if n := utf8.RuneCountInString(p.Dek); n > 161 || !strings.HasSuffix(p.Dek, "…") {
		t.Errorf("Dek = %q (%d runes)", p.Dek, n)
	}
}

func TestFromEmailEscapesPlainText(t *testing.T) {
	p := FromEmail(parse(t, "From: a@example.com\r\nSubject: x\r\nContent-Type: text/plain\r\n\r\n<script>alert(1)</script>\r\n"))
	if strings.Contains(string(p.HTML), "<script") {
		t.Errorf("HTML = %s", p.HTML)
	}
}

func TestFromEmailMultipartDekSkipsChrome(t *testing.T) {
	raw := "From: News <news@example.com>\r\nSubject: Weekly\r\nMIME-Version: 1.0\r\n" +
		"Content-Type: multipart/alternative; boundary=XX\r\n\r\n" +
		"--XX\r\nContent-Type: text/plain; charset=utf-8\r\n\r\n" +
		"View this email in your browser (https://ex.com/web)\r\n\r\nThe real first sentence. More words.\r\n" +
		"--XX\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<p><a href="https://ex.com/web">View this email in your browser</a></p><p>The real first sentence. More words.</p>` + "\r\n" +
		"--XX--\r\n"
	p := FromEmail(parse(t, raw))
	if p.Dek != "The real first sentence." {
		t.Errorf("Dek = %q", p.Dek)
	}
	if strings.Contains(p.Text, "browser") || p.Words != 6 {
		t.Errorf("Text = %q, Words = %d", p.Text, p.Words)
	}
}

func TestFromEmailHTMLTextDecodesEntitiesAndSkipsHidden(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<div style="display:none">secret preview</div><p><a href="https://ex.com/v">View in browser</a></p>` +
		`<p>It&#8217;s here &mdash; finally.</p><p>Second para.</p>`
	p := FromEmail(parse(t, raw))
	if p.Text != "It’s here — finally.\n\nSecond para." {
		t.Errorf("Text = %q", p.Text)
	}
	if p.Words != 6 {
		t.Errorf("Words = %d, want 6", p.Words)
	}
	if p.Dek != "secret preview" {
		t.Errorf("Dek = %q", p.Dek)
	}
}

func TestFromEmailHTMLOnlyDekIsFirstSentence(t *testing.T) {
	raw := "From: a@example.com\r\nSubject: x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<p><a href="https://ex.com/v">View in browser</a></p><p>Hello&nbsp;there&#8217;s more. Rest.</p>`
	if p := FromEmail(parse(t, raw)); p.Dek != "Hello there’s more." {
		t.Errorf("Dek = %q", p.Dek)
	}
}

func TestFromEmailPreheaderIsTidiedAndCapped(t *testing.T) {
	pad := strings.Repeat("&nbsp;&#847;&zwnj;&#8203;&#65279;", 40)
	raw := "From: a@example.com\r\nSubject: x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<div style="display:none">Rates,   again.` + pad + `</div><p>Body.</p>`
	if p := FromEmail(parse(t, raw)); p.Dek != "Rates, again." {
		t.Errorf("Dek = %q", p.Dek)
	}

	long := strings.Repeat("word ", 60)
	raw = "From: a@example.com\r\nSubject: x\r\nContent-Type: text/html; charset=utf-8\r\n\r\n" +
		`<div style="display:none">` + long + `</div><p>Body.</p>`
	p := FromEmail(parse(t, raw))
	if n := utf8.RuneCountInString(p.Dek); n > 161 || !strings.HasSuffix(p.Dek, "…") {
		t.Errorf("Dek = %q (%d runes)", p.Dek, n)
	}
}
