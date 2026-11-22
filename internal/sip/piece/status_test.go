package piece

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"nmail/internal/sip/mastodon"
)

func homeStatus(t *testing.T, id string) mastodon.Status {
	t.Helper()
	b, err := os.ReadFile("../mastodon/testdata/home.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []mastodon.Status
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	for _, s := range all {
		if s.ID == id {
			return s
		}
	}
	t.Fatalf("no status %s", id)
	return mastodon.Status{}
}

func ts(s string) time.Time {
	v, err := time.Parse(time.RFC3339, s)
	if err != nil {
		panic(err)
	}
	return v
}

func TestFromStatusPost(t *testing.T) {
	s := homeStatus(t, "200")
	p := FromStatus(s)
	if p.ID != s.URI || p.Title != "" {
		t.Errorf("ID/Title = %q/%q", p.ID, p.Title)
	}
	if p.Dek != "First pour-over of the season." {
		t.Errorf("Dek = %q", p.Dek)
	}
	if p.Author != "Alice" || p.Source != "mastodon.social" || p.Channel != "fediverse" {
		t.Errorf("Author/Source/Channel = %q/%q/%q", p.Author, p.Source, p.Channel)
	}
	want := ts("2026-10-15T09:00:00Z")
	if !p.Published.Equal(want) || !p.Arrived.Equal(want) {
		t.Errorf("Published/Arrived = %v/%v", p.Published, p.Arrived)
	}
	if !strings.Contains(p.Text, "The kettle finally arrived.") {
		t.Errorf("Text = %q", p.Text)
	}
	if p.Words != len(strings.Fields(p.Text)) {
		t.Errorf("Words = %d", p.Words)
	}
	if p.Original != "https://mastodon.social/@alice/200" || p.BoostedBy != "" {
		t.Errorf("Original/BoostedBy = %q/%q", p.Original, p.BoostedBy)
	}
	img := `<img src="https://files.mastodon.social/kettle.jpg" alt="A gooseneck kettle pouring water over a paper filter."`
	if !strings.Contains(string(p.HTML), img) {
		t.Errorf("HTML = %s", p.HTML)
	}
	if p.Raw != nil {
		t.Errorf("Raw = %v", p.Raw)
	}
	if p.Headline() != p.Dek {
		t.Errorf("Headline = %q", p.Headline())
	}
}

func TestFromStatusBoost(t *testing.T) {
	s := homeStatus(t, "300")
	p := FromStatus(s)
	if p.ID != s.Reblog.URI {
		t.Errorf("ID = %q", p.ID)
	}
	if p.Author != "Bob's notebook" || p.Source != "write.as" || p.BoostedBy != "Carol" {
		t.Errorf("Author/Source/BoostedBy = %q/%q/%q", p.Author, p.Source, p.BoostedBy)
	}
	if !p.Published.Equal(ts("2026-10-08T09:00:00Z")) || !p.Arrived.Equal(ts("2026-10-15T11:00:00Z")) {
		t.Errorf("Published/Arrived = %v/%v", p.Published, p.Arrived)
	}
	h := string(p.HTML)
	if !strings.Contains(h, `<a href="https://write.as/bob/autumn-reading"`) || !strings.Contains(h, "Autumn reading") {
		t.Errorf("HTML = %s", h)
	}
}

func TestFromStatusContentWarning(t *testing.T) {
	if got := FromStatus(homeStatus(t, "100")).Dek; got != "Spoilers for the last chapter" {
		t.Errorf("Dek = %q", got)
	}
}

func TestFromStatusFallbacks(t *testing.T) {
	post := mastodon.Status{
		URI:     "https://x.example/users/z/statuses/1",
		Account: mastodon.Account{Acct: "z@x.example"},
		Content: "<p>hello</p>",
	}
	boost := mastodon.Status{Account: mastodon.Account{Acct: "b@y.example"}, Reblog: &post}
	for _, p := range []Piece{FromStatus(post), FromStatus(boost)} {
		if p.Author != "z@x.example" || p.Source != "x.example" || p.Original != post.URI {
			t.Errorf("Author/Source/Original = %q/%q/%q", p.Author, p.Source, p.Original)
		}
	}
	if got := FromStatus(boost).BoostedBy; got != "b@y.example" {
		t.Errorf("BoostedBy = %q", got)
	}
}

func TestFromStatusIsSafe(t *testing.T) {
	s := mastodon.Status{
		URL: "javascript:x", URI: "tag:x",
		Content: "<p>hi</p>",
		Attachments: []mastodon.Attachment{
			{Type: "image", URL: "javascript:alert(1)"},
			{Type: "image", URL: ""},
			{Type: "audio", URL: "https://x.example/a.mp3", Description: `a "song" <b>`},
		},
		Card: &mastodon.Card{Title: "t", URL: "javascript:x"},
	}
	p := FromStatus(s)
	h := string(p.HTML)
	if strings.Contains(h, "javascript:") {
		t.Errorf("javascript in %s", h)
	}
	if !strings.Contains(h, `<a href="https://x.example/a.mp3"`) || !strings.Contains(h, "&lt;b&gt;") || strings.Contains(h, "<b>") {
		t.Errorf("HTML = %s", h)
	}
	if p.Original != "" {
		t.Errorf("Original = %q", p.Original)
	}
}

func TestFromStatusStripsScript(t *testing.T) {
	p := FromStatus(mastodon.Status{Content: "<p>hi</p><script>x()</script>"})
	if strings.Contains(string(p.HTML), "script") {
		t.Errorf("HTML = %s", p.HTML)
	}
}

func TestFromStatusImageOnly(t *testing.T) {
	post := mastodon.Status{
		URI:     "https://x.example/users/z/statuses/2",
		Account: mastodon.Account{Acct: "z@x.example"},
		Attachments: []mastodon.Attachment{
			{Type: "image", URL: "https://x.example/a.png", Description: "A red fox in the snow"},
		},
	}
	if got := FromStatus(post).Dek; got != "A red fox in the snow" {
		t.Errorf("Dek with description = %q", got)
	}
	post.Attachments[0].Description = ""
	if got := FromStatus(post).Dek; got != "Photo" {
		t.Errorf("Dek without description = %q", got)
	}
}
