package piece

import (
	"html"
	"net/url"
	"strings"

	"nmail/internal/sip/mastodon"
)

// FromStatus turns a home-timeline status into a Piece. For a boost the
// shown post is the reblog and the outer status says who boosted it.
func FromStatus(s mastodon.Status) Piece {
	post := s
	p := Piece{Channel: "fediverse", Arrived: s.CreatedAt}
	if s.Reblog != nil {
		post = *s.Reblog
		p.BoostedBy = accountName(s.Account)
	}
	p.ID = post.URI
	p.Author = accountName(post.Account)
	p.Source = accountHost(post.Account)
	p.Published = post.CreatedAt

	var b strings.Builder
	b.WriteString(post.Content)
	for _, a := range post.Attachments {
		if !isHTTP(a.URL) {
			continue
		}
		u, d := html.EscapeString(a.URL), html.EscapeString(a.Description)
		if a.Type == "image" {
			b.WriteString(`<p><img src="` + u + `" alt="` + d + `"></p>`)
			continue
		}
		if d == "" {
			d = "attachment"
		}
		b.WriteString(`<p><a href="` + u + `">` + d + `</a></p>`)
	}
	if c := post.Card; c != nil && isHTTP(c.URL) {
		title := c.Title
		if title == "" {
			title = c.URL
		}
		b.WriteString(`<p><a href="` + html.EscapeString(c.URL) + `">` + html.EscapeString(title) + `</a></p>`)
	}
	cleaned := Clean(b.String())
	p.HTML, p.Text = cleaned.HTML, cleaned.Text
	p.Words = len(strings.Fields(p.Text))

	p.Dek = clip(post.SpoilerText)
	if p.Dek == "" {
		p.Dek = firstSentence(p.Text)
	}
	for _, u := range []string{post.URL, post.URI} {
		if isHTTP(u) {
			p.Original = u
			break
		}
	}
	return p
}

func accountName(a mastodon.Account) string {
	if a.DisplayName != "" {
		return a.DisplayName
	}
	return a.Acct
}

func accountHost(a mastodon.Account) string {
	if u, err := url.Parse(a.URL); err == nil && u.Hostname() != "" {
		return u.Hostname()
	}
	return domain(a.Acct)
}

func isHTTP(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "http" || u.Scheme == "https") && u.Host != ""
}
