package piece

import (
	"bytes"
	"html/template"
	"regexp"
	"strconv"
	"strings"

	"github.com/microcosm-cc/bluemonday"
	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// Cleaned is an email body reduced to safe, plain markup.
type Cleaned struct {
	HTML      template.HTML
	Text      string // the visible text of HTML: paragraphs split by blank lines
	Preheader string // text of the first hidden element with text, tidied
	ViewLink  string // href of a "view in browser"/"view online"/"read online" link, http(s) only
}

// Clean strips an HTML email down to its reading: no layout, styling,
// scripts, tracking pixels, hidden text or "view in browser" link. The
// result is emitted unescaped, so the policy below is a security boundary.
func Clean(src string) Cleaned {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Cleaned{}
	}
	var c Cleaned
	var drop []*html.Node
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode {
			switch {
			case n.DataAtom == atom.Head || n.DataAtom == atom.Style || n.DataAtom == atom.Script:
				return
			case hidden(n):
				if c.Preheader == "" {
					c.Preheader = tidy(textOf(n))
				}
				drop = append(drop, n)
				return
			case n.DataAtom == atom.Img && !showable(n):
				drop = append(drop, n)
				return
			case n.DataAtom == atom.A && isViewLink(n):
				if c.ViewLink == "" {
					c.ViewLink = strings.TrimSpace(attr(n, "href"))
				}
				drop = append(drop, viewLinkNode(n))
				return
			}
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	walk(root)
	for _, n := range drop {
		if n.Parent != nil {
			n.Parent.RemoveChild(n)
		}
	}

	var buf bytes.Buffer
	if body := find(root, atom.Body); body != nil {
		for ch := body.FirstChild; ch != nil; ch = ch.NextSibling {
			if html.Render(&buf, ch) != nil {
				return Cleaned{}
			}
		}
	}
	c.HTML = template.HTML(strings.TrimSpace(policy.Sanitize(buf.String())))
	c.Text = plainText(string(c.HTML))
	return c
}

// plainText is the text of sanitized markup, entities decoded, one
// paragraph per block element and a line break per <br>.
func plainText(markup string) string {
	nodes, err := html.ParseFragment(strings.NewReader(markup), &html.Node{Type: html.ElementNode, Data: "body", DataAtom: atom.Body})
	if err != nil {
		return ""
	}
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		switch {
		case n.Type == html.TextNode:
			b.WriteString(reSpace.ReplaceAllString(n.Data, " "))
		case n.DataAtom == atom.Br:
			b.WriteString("\n")
		case n.Type == html.ElementNode && block[n.DataAtom]:
			b.WriteString("\n\n")
			defer b.WriteString("\n\n")
		}
		for ch := n.FirstChild; ch != nil; ch = ch.NextSibling {
			walk(ch)
		}
	}
	for _, n := range nodes {
		walk(n)
	}
	lines := strings.Split(b.String(), "\n")
	for i, l := range lines {
		lines[i] = tidy(l)
	}
	return strings.TrimSpace(reBlankLines.ReplaceAllString(strings.Join(lines, "\n"), "\n\n"))
}

var block = map[atom.Atom]bool{
	atom.P: true, atom.H1: true, atom.H2: true, atom.H3: true, atom.H4: true, atom.H5: true, atom.H6: true,
	atom.Ul: true, atom.Ol: true, atom.Li: true, atom.Blockquote: true, atom.Pre: true, atom.Hr: true,
	atom.Figure: true, atom.Figcaption: true,
}

// tidy drops the invisible characters newsletters pad previews with and
// collapses whitespace.
func tidy(s string) string {
	return collapse(strings.Map(func(r rune) rune {
		switch r {
		case '\u034f', '\u00ad', '\u200b', '\u200c', '\u200d', '\u2060', '\ufeff':
			return -1
		}
		return r
	}, s))
}

var policy = newPolicy()

func newPolicy() *bluemonday.Policy {
	p := bluemonday.NewPolicy()
	p.AllowElements("h1", "h2", "h3", "h4", "h5", "h6", "p", "br", "hr", "ul", "ol", "li",
		"blockquote", "pre", "code", "em", "strong", "b", "i", "a", "img", "figure", "figcaption")
	p.AllowAttrs("href").OnElements("a")
	p.AllowAttrs("src", "alt").OnElements("img")
	p.AllowURLSchemes("http", "https", "mailto")
	p.AddTargetBlankToFullyQualifiedLinks(true)
	p.RequireNoReferrerOnLinks(true)
	return p
}

var (
	reHidden     = regexp.MustCompile(`(?i)display\s*:\s*none|visibility\s*:\s*hidden|mso-hide\s*:\s*all|(?:max-height|font-size)\s*:\s*0(?:px|pt|em|rem|%)?\s*(?:;|!|$)`)
	reViewLink   = regexp.MustCompile(`(?i)^(?:view|read)\s+(?:this\s+|the\s+|email\s+|message\s+)*(?:in\s+(?:your\s+|a\s+)?browser|online)\W*$`)
	reStyleSize  = regexp.MustCompile(`(?i)(?:^|[;\s])(?:width|height)\s*:\s*([\d.]+(?:px)?)\s*(?:;|!|$)`)
	reSpace      = regexp.MustCompile(`[ \t\r\n\f\x{a0}]+`)
	reBlankLines = regexp.MustCompile(`\n{3,}`)
)

// hidden reports elements mail clients don't show, such as preheaders.
func hidden(n *html.Node) bool {
	return reHidden.MatchString(attr(n, "style")) || hasAttr(n, "hidden")
}

// showable reports whether an image is worth keeping: not a tracking pixel
// and fetchable over http(s).
func showable(img *html.Node) bool {
	sizes := []string{attr(img, "width"), attr(img, "height")}
	for _, m := range reStyleSize.FindAllStringSubmatch(attr(img, "style"), -1) {
		sizes = append(sizes, m[1])
	}
	for _, v := range sizes {
		v = strings.TrimSuffix(strings.ToLower(strings.TrimSpace(v)), "px")
		if n, err := strconv.ParseFloat(v, 64); err == nil && n <= 2 {
			return false
		}
	}
	src := strings.ToLower(strings.TrimSpace(attr(img, "src")))
	return strings.HasPrefix(src, "http://") || strings.HasPrefix(src, "https://")
}

func isViewLink(a *html.Node) bool {
	href := strings.ToLower(strings.TrimSpace(attr(a, "href")))
	if !strings.HasPrefix(href, "http://") && !strings.HasPrefix(href, "https://") {
		return false
	}
	return reViewLink.MatchString(collapse(textOf(a)))
}

// viewLinkNode is what to remove for a view link: its nearest enclosing
// p, div, td or tr when that holds nothing but the link text, else the
// anchor alone.
func viewLinkNode(a *html.Node) *html.Node {
	for p := a.Parent; p != nil; p = p.Parent {
		switch p.DataAtom {
		case atom.P, atom.Div, atom.Td, atom.Tr:
			if collapse(textOf(p)) == collapse(textOf(a)) {
				return p
			}
			return a
		case atom.Body, atom.Html:
			return a
		}
	}
	return a
}

func collapse(s string) string { return strings.TrimSpace(reSpace.ReplaceAllString(s, " ")) }

func find(n *html.Node, a atom.Atom) *html.Node {
	if n.Type == html.ElementNode && n.DataAtom == a {
		return n
	}
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		if f := find(c, a); f != nil {
			return f
		}
	}
	return nil
}

func attr(n *html.Node, key string) string {
	for _, a := range n.Attr {
		if a.Key == key {
			return a.Val
		}
	}
	return ""
}

func hasAttr(n *html.Node, key string) bool {
	for _, a := range n.Attr {
		if a.Key == key {
			return true
		}
	}
	return false
}

// textOf returns the visible text under n.
func textOf(n *html.Node) string {
	var b strings.Builder
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.TextNode {
			b.WriteString(n.Data)
			return
		}
		if n.Type == html.ElementNode && (n.DataAtom == atom.Style || n.DataAtom == atom.Script) {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}
