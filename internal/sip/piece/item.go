package piece

import (
	"html"
	"net/url"
	"strings"

	"nmail/internal/sip/stw"
)

// FromItem turns a fediverse item into a Piece.
func FromItem(it stw.Item) Piece {
	p := Piece{
		ID:        it.ID,
		Title:     it.Title,
		Channel:   "fediverse",
		Published: it.Published,
		Arrived:   it.ReceivedAt,
		Author:    it.Author.Name,
		Source:    itemSource(it.Author),
	}
	if p.Published.IsZero() {
		p.Published = it.ReceivedAt
	}
	if p.Author == "" {
		p.Author = it.Author.Handle
	}
	if it.Boost != nil {
		p.BoostedBy = it.Boost.By.Name
		if p.BoostedBy == "" {
			p.BoostedBy = it.Boost.By.Handle
		}
	}

	cleaned := Clean(it.ContentHTML + attachmentMarkup(it.Attachments))
	p.HTML = cleaned.HTML
	p.Text = it.ContentText
	if p.Text == "" {
		p.Text = cleaned.Text
	}
	p.Words = len(strings.Fields(p.Text))

	p.Dek = clip(it.Summary)
	if p.Dek == "" {
		p.Dek = firstSentence(p.Text)
	}
	if isWebURL(it.URL) {
		p.Original = it.URL
	}
	return p
}

// itemSource is the host of the author's id, else of their handle.
func itemSource(a stw.Account) string {
	if u, err := url.Parse(a.ID); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	if i := strings.LastIndex(a.Handle, "@"); i >= 0 {
		return a.Handle[i+1:]
	}
	return ""
}

func isWebURL(s string) bool {
	u, err := url.Parse(s)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}

// attachmentMarkup renders http(s) attachments: images inline, the rest as
// links. Clean has the last word on what survives.
func attachmentMarkup(as []stw.Attachment) string {
	var b strings.Builder
	for _, a := range as {
		if !isWebURL(a.URL) {
			continue
		}
		src, desc := html.EscapeString(a.URL), html.EscapeString(a.Description)
		if a.Kind == "image" {
			b.WriteString(`<p><img src="` + src + `" alt="` + desc + `"></p>`)
			continue
		}
		if desc == "" {
			desc = "attachment"
		}
		b.WriteString(`<p><a href="` + src + `">` + desc + `</a></p>`)
	}
	return b.String()
}
