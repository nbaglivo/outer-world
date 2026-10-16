package piece

import (
	"html"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"nmail/internal/mailbox"
)

const dekRunes = 160

var (
	reTags    = regexp.MustCompile(`^(\s*\[[^\]]*\]\s*)+`)
	reParas   = regexp.MustCompile(`\n\s*\n`)
	reSentEnd = regexp.MustCompile(`[.!?] |\n`)
)

// FromEmail turns a newsletter into a Piece.
func FromEmail(m *mailbox.Message) Piece {
	p := Piece{
		ID:        m.MessageID,
		Title:     strings.TrimSpace(reTags.ReplaceAllString(m.Subject, "")),
		Channel:   "email",
		Published: m.Date,
		Arrived:   m.Arrived,
		Raw:       m.Raw,
	}
	if p.ID == "" {
		p.ID = "uid:" + strconv.FormatUint(uint64(m.UID), 10)
	}
	if p.Published.IsZero() {
		p.Published = m.Arrived
	}

	author := ""
	if m.From != m.FromAddr {
		author = m.From
	}
	p.Author = author
	if p.Author == "" {
		p.Author = m.FromAddr
	}
	p.Source = listName(m.ListID)
	if p.Source == "" {
		p.Source = author
	}
	if p.Source == "" {
		p.Source = domain(m.FromAddr)
	}

	var cleaned Cleaned
	if m.HTML != "" {
		cleaned = Clean(m.HTML)
		p.HTML, p.Text = cleaned.HTML, cleaned.Text
	} else {
		p.HTML, p.Text = paragraphs(m.Text), m.Text
	}
	p.Words = len(strings.Fields(p.Text))

	p.Dek = clip(cleaned.Preheader)
	if p.Dek == "" {
		p.Dek = firstSentence(p.Text)
	}
	if post := strings.TrimSpace(m.ListPost); strings.HasPrefix(post, "<http") && strings.HasSuffix(post, ">") {
		p.Original = strings.Trim(post, "<>")
	} else {
		p.Original = cleaned.ViewLink
	}
	return p
}

// listName is the description part of a List-Id value, unquoted.
func listName(listID string) string {
	i := strings.Index(listID, "<")
	if i < 0 {
		return ""
	}
	return strings.Trim(strings.TrimSpace(listID[:i]), `"`)
}

func domain(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}

// paragraphs turns plain text into escaped <p> paragraphs.
func paragraphs(text string) template.HTML {
	var b strings.Builder
	for _, para := range reParas.Split(strings.ReplaceAll(text, "\r\n", "\n"), -1) {
		if para = strings.TrimSpace(para); para != "" {
			b.WriteString("<p>" + html.EscapeString(para) + "</p>\n")
		}
	}
	return template.HTML(strings.TrimSpace(b.String()))
}

// firstSentence is the text up to the first ". ", "! ", "? " or newline,
// clipped.
func firstSentence(text string) string {
	text = strings.TrimSpace(text)
	if loc := reSentEnd.FindStringIndex(text); loc != nil {
		end := loc[0]
		if text[end] != '\n' {
			end++ // keep the punctuation
		}
		text = text[:end]
	}
	return clip(tidy(text))
}

// clip cuts text at dekRunes on a word boundary with an ellipsis.
func clip(text string) string {
	r := []rune(text)
	if len(r) <= dekRunes {
		return text
	}
	cut := string(r[:dekRunes])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return cut + "…"
}
