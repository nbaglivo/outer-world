package piece

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
	"time"

	"nmail/internal/sip/stw"
)

func morningItems(t *testing.T) []stw.Item {
	t.Helper()
	b, err := os.ReadFile("../stw/testdata/morning.json")
	if err != nil {
		t.Fatal(err)
	}
	var page struct {
		Items []stw.Item `json:"items"`
	}
	if err := json.Unmarshal(b, &page); err != nil {
		t.Fatal(err)
	}
	return page.Items
}

func TestFromItemNote(t *testing.T) {
	it := morningItems(t)[0]
	p := FromItem(it)
	if p.ID != it.ID || p.Title != "" || p.Author != "Alice" || p.Source != "mastodon.social" || p.Channel != "fediverse" {
		t.Errorf("identity fields: %+v", p)
	}
	if p.Dek != "First pour-over of the season." {
		t.Errorf("Dek = %q", p.Dek)
	}
	if !p.Published.Equal(it.Published) || !p.Arrived.Equal(it.ReceivedAt) {
		t.Errorf("times: %v %v", p.Published, p.Arrived)
	}
	if p.Text != it.ContentText || p.Words != 10 {
		t.Errorf("text/words: %q %d", p.Text, p.Words)
	}
	if p.Original != "https://mastodon.social/@alice/113284750912345678" || p.BoostedBy != "" || p.Raw != nil {
		t.Errorf("original/boost/raw: %q %q %v", p.Original, p.BoostedBy, p.Raw)
	}
	h := string(p.HTML)
	for _, want := range []string{
		"First pour-over",
		`<img src="https://files.mastodon.social/media_attachments/files/113/284/750/original/kettle.jpg" alt="A gooseneck kettle pouring water over a paper filter."`,
	} {
		if !strings.Contains(h, want) {
			t.Errorf("HTML missing %q in %s", want, h)
		}
	}
}

func TestFromItemBoostedArticle(t *testing.T) {
	p := FromItem(morningItems(t)[1])
	if p.Title != "Autumn reading" || p.Dek != "Five books for long evenings." ||
		p.Author != "Bob's notebook" || p.Source != "write.as" || p.BoostedBy != "Carol" {
		t.Errorf("%+v", p)
	}
}

func TestFromItemSensitiveDek(t *testing.T) {
	p := FromItem(stw.Item{
		Sensitive:   true,
		Summary:     "Spoilers for the last chapter",
		ContentText: "The river was the narrator all along.",
	})
	if p.Dek != "Spoilers for the last chapter" {
		t.Errorf("Dek = %q", p.Dek)
	}
}

func TestFromItemSetsSensitive(t *testing.T) {
	if p := FromItem(stw.Item{Sensitive: true, Summary: "cw"}); !p.Sensitive {
		t.Errorf("Sensitive = false")
	}
	if p := FromItem(stw.Item{ContentText: "plain"}); p.Sensitive {
		t.Errorf("Sensitive = true")
	}
}

func TestFromItemImageOnlyDek(t *testing.T) {
	it := stw.Item{
		Author: stw.Account{Name: "Alice"},
		Attachments: []stw.Attachment{
			{URL: "https://x.example/a.png", Kind: "image"},
			{URL: "https://x.example/b.png", Kind: "image", Description: "A red kite over a beach"},
		},
	}
	if p := FromItem(it); p.Dek != "A red kite over a beach" {
		t.Errorf("Dek = %q", p.Dek)
	}
	it.Attachments = it.Attachments[:1]
	if p := FromItem(it); p.Dek != "A post by Alice" {
		t.Errorf("Dek = %q", p.Dek)
	}
}

func TestFromItemNulls(t *testing.T) {
	recv := time.Date(2026, 10, 16, 6, 0, 0, 0, time.UTC)
	p := FromItem(stw.Item{
		ContentText: "Hello there.",
		Author:      stw.Account{ID: "https://x.example/users/z", Handle: "@z@x.example"},
		Boost:       &stw.Boost{By: stw.Account{Handle: "@y@y.example"}},
		ReceivedAt:  recv,
	})
	if !p.Published.Equal(recv) {
		t.Errorf("Published = %v", p.Published)
	}
	if p.Author != "@z@x.example" || p.Source != "x.example" {
		t.Errorf("author/source: %q %q", p.Author, p.Source)
	}
	if p.Headline() == "" {
		t.Error("empty headline")
	}
	if p.BoostedBy != "@y@y.example" {
		t.Errorf("BoostedBy = %q", p.BoostedBy)
	}
}

func TestFromItemSourceFromHandle(t *testing.T) {
	p := FromItem(stw.Item{Author: stw.Account{Handle: "@z@x.example"}})
	if p.Source != "x.example" {
		t.Errorf("Source = %q", p.Source)
	}
}

func TestFromItemAttachmentsAreSafe(t *testing.T) {
	p := FromItem(stw.Item{
		URL:         "javascript:x",
		ContentHTML: "<p>hi</p>",
		Attachments: []stw.Attachment{
			{URL: "javascript:alert(1)", Kind: "image"},
			{URL: "https://x.example/a.mp3", Kind: "audio", Description: `a "song" <b>`},
		},
	})
	h := string(p.HTML)
	if strings.Contains(h, "javascript:") {
		t.Errorf("javascript in %s", h)
	}
	if !strings.Contains(h, `<a href="https://x.example/a.mp3"`) {
		t.Errorf("link missing in %s", h)
	}
	if !strings.Contains(h, "&lt;b&gt;") || strings.Contains(h, "<b>") {
		t.Errorf("description not escaped in %s", h)
	}
	if p.Original != "" {
		t.Errorf("Original = %q", p.Original)
	}
}

func TestFromItemStripsScript(t *testing.T) {
	p := FromItem(stw.Item{ContentHTML: "<p>hi</p><script>x()</script>"})
	if strings.Contains(string(p.HTML), "script") || !strings.Contains(string(p.HTML), "hi") {
		t.Errorf("HTML = %s", p.HTML)
	}
}
