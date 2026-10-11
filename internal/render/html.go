package render

import (
	"regexp"
	"strings"

	"golang.org/x/net/html"
	"golang.org/x/net/html/atom"
)

// HTML renders an HTML email body. Links become navigable; links styled as
// buttons (see isButton) are drawn as buttons.
func HTML(src string) Doc { return HTMLWith(src, Options{}) }

// Options tune HTML rendering for a particular kind of message.
type Options struct {
	// Chip reports links to draw as small buttons though the sender didn't
	// style them as buttons.
	Chip func(Link) bool
}

const chipColor = "238"

func HTMLWith(src string, opts Options) Doc {
	root, err := html.Parse(strings.NewReader(src))
	if err != nil {
		return Text(src)
	}
	b := &builder{link: -1, opts: opts}
	b.walk(root)
	b.flush()
	return b.doc()
}

type builder struct {
	lines  []Line
	links  []Link
	cur    Line
	style  Style
	link   int
	indent int
	bar    string
	pre    bool
	opts   Options
}

func (b *builder) newLine() Line { return Line{Indent: b.indent, Bar: b.bar} }

func (b *builder) doc() Doc {
	lines := b.lines
	for len(lines) > 0 && blank(lines[len(lines)-1]) {
		lines = lines[:len(lines)-1]
	}
	return Doc{Lines: lines, Links: b.links}
}

func blank(l Line) bool { return len(l.Spans) == 0 }

// flush ends the current line, if it has anything on it.
func (b *builder) flush() {
	if len(b.cur.Spans) > 0 {
		b.lines = append(b.lines, b.cur)
	}
	b.cur = b.newLine()
}

// gap ends the current line and leaves one blank line, never more.
func (b *builder) gap() {
	b.flush()
	if n := len(b.lines); n > 0 && !blank(b.lines[n-1]) {
		b.lines = append(b.lines, Line{Bar: b.bar})
	}
}

func (b *builder) trimBlank() {
	for n := len(b.lines); n > 0 && blank(b.lines[n-1]); n-- {
		b.lines = b.lines[:n-1]
	}
}

var reSpace = regexp.MustCompile(`[ \t\r\n\f\x{a0}]+`)

func (b *builder) text(s string) {
	if !b.pre {
		s = reSpace.ReplaceAllString(s, " ")
		if b.atLineStart() || b.endsWithSpace() {
			s = strings.TrimLeft(s, " ")
		}
	}
	if s == "" {
		return
	}
	if b.pre && strings.Contains(s, "\n") {
		parts := strings.Split(s, "\n")
		for i, p := range parts {
			if i > 0 {
				b.lines = append(b.lines, b.cur)
				b.cur = b.newLine()
			}
			b.span(p, b.style)
		}
		return
	}
	b.span(s, b.style)
}

func (b *builder) span(s string, st Style) {
	if s == "" {
		return
	}
	spans := b.cur.Spans
	if n := len(spans); n > 0 && spans[n-1].Style == st && spans[n-1].Link == b.link && st != ButtonText {
		spans[n-1].Text += s
		return
	}
	b.cur.Spans = append(spans, Span{Text: s, Style: st, Link: b.link})
}

func (b *builder) atLineStart() bool {
	for _, s := range b.cur.Spans {
		if strings.TrimSpace(s.Text) != "" {
			return false
		}
	}
	return true
}

func (b *builder) endsWithSpace() bool {
	n := len(b.cur.Spans)
	return n > 0 && strings.HasSuffix(b.cur.Spans[n-1].Text, " ")
}

func (b *builder) afterButton() bool {
	for i := len(b.cur.Spans) - 1; i >= 0; i-- {
		if s := b.cur.Spans[i]; strings.TrimSpace(s.Text) != "" || s.Style == ButtonText {
			return s.Style == ButtonText
		}
	}
	return false
}

// space separates inline content, e.g. adjacent table cells.
func (b *builder) space() {
	if !b.atLineStart() && !b.endsWithSpace() {
		b.span(" ", Plain)
	}
}

func (b *builder) walk(n *html.Node) {
	switch n.Type {
	case html.TextNode:
		b.text(n.Data)
		return
	case html.ElementNode:
		b.element(n)
		return
	}
	b.children(n)
}

func (b *builder) children(n *html.Node) {
	for c := n.FirstChild; c != nil; c = c.NextSibling {
		b.walk(c)
	}
}

// withStyle walks n's children in style st, unless already inside a link.
func (b *builder) withStyle(n *html.Node, st Style) {
	saved := b.style
	if b.link < 0 {
		b.style = st
	}
	b.children(n)
	b.style = saved
}

func (b *builder) element(n *html.Node) {
	if hidden(n) {
		return
	}
	if n.DataAtom == atom.A {
		b.block(n)
		return
	}
	if bar := leftBorder(n); bar != "" {
		// A box with a colored left edge: a card or a quote.
		b.gap()
		saved := b.bar
		b.bar = bar
		b.cur = b.newLine()
		b.block(n)
		b.flush()
		b.trimBlank()
		b.bar = saved
		b.cur = b.newLine()
		b.gap()
		return
	}
	if st, ok := textStyle(n); ok && b.link < 0 {
		saved := b.style
		b.style = st
		b.block(n)
		b.style = saved
		return
	}
	b.block(n)
}

// block renders n by what kind of element it is.
func (b *builder) block(n *html.Node) {
	switch n.DataAtom {
	case atom.Head, atom.Style, atom.Script, atom.Title, atom.Template, atom.Img:
		return
	case atom.Br:
		if len(b.cur.Spans) == 0 {
			b.gap()
		}
		b.flush()
	case atom.Hr:
		b.flush()
		hr := b.newLine()
		hr.Spans = []Span{{Style: HRule, Link: -1}}
		b.lines = append(b.lines, hr)
	case atom.A:
		b.anchor(n)
	case atom.B, atom.Strong:
		b.withStyle(n, Bold)
	case atom.I, atom.Em:
		b.withStyle(n, Italic)
	case atom.Small:
		b.withStyle(n, Muted)
	case atom.H1, atom.H2, atom.H3, atom.H4, atom.H5, atom.H6:
		b.gap()
		b.withStyle(n, Heading)
		b.gap()
	case atom.P, atom.Table, atom.Ul, atom.Ol, atom.Blockquote:
		b.gap()
		if n.DataAtom == atom.Blockquote || n.DataAtom == atom.Ul || n.DataAtom == atom.Ol {
			b.indent += 2
			b.cur = b.newLine()
			b.children(n)
			b.flush()
			b.indent -= 2
		} else {
			b.children(n)
		}
		b.gap()
	case atom.Li:
		b.flush()
		b.span("• ", Muted)
		b.cur.Hang = 2
		b.children(n)
		b.flush()
	case atom.Pre:
		b.gap()
		b.pre = true
		b.children(n)
		b.pre = false
		b.gap()
	case atom.Td, atom.Th:
		b.space()
		b.children(n)
		b.space()
	case atom.Div, atom.Tr, atom.Section, atom.Article, atom.Header, atom.Footer,
		atom.Center, atom.Main, atom.Nav, atom.Dl, atom.Dt, atom.Dd, atom.Figure:
		b.flush()
		b.children(n)
		b.flush()
	default:
		b.children(n)
	}
}

func (b *builder) anchor(n *html.Node) {
	href := strings.TrimSpace(attr(n, "href"))
	label := strings.TrimSpace(reSpace.ReplaceAllString(textOf(n), " "))
	if !followable(href) || b.link >= 0 {
		b.children(n)
		return
	}
	if label == "" {
		label = strings.TrimSpace(altOf(n))
	}
	if label == "" {
		return // an image-only link with no alt text: nothing to show
	}

	link := Link{URL: href, Text: label, Button: isButton(n)}
	if link.Button {
		link.Color = buttonColor(n)
	} else if b.opts.Chip != nil && b.opts.Chip(link) {
		link.Button, link.Color = true, chipColor
	}
	b.links = append(b.links, link)
	if link.Button {
		b.space()
		if b.afterButton() {
			b.span(" ", Plain) // two columns between buttons side by side
		}
		b.link = len(b.links) - 1
		b.span(" "+label+" ", ButtonText)
		b.link = -1
		b.space()
		return
	}
	saved := b.style
	b.link, b.style = len(b.links)-1, LinkText
	if strings.TrimSpace(textOf(n)) == "" {
		b.text(label)
	} else {
		b.children(n)
	}
	b.link, b.style = -1, saved
}

func followable(href string) bool {
	h := strings.ToLower(href)
	return strings.HasPrefix(h, "http://") || strings.HasPrefix(h, "https://") || strings.HasPrefix(h, "mailto:")
}

var (
	reButtonClass = regexp.MustCompile(`(?i)\b(btn|button|cta)\b`)
	reBackground  = regexp.MustCompile(`(?i)background(-color)?\s*:\s*[^;]*[#a-z0-9]`)
	reBorder      = regexp.MustCompile(`(?i)border(-radius)?\s*:\s*[1-9]`)
)

// isButton reports whether a link is styled as a button: it says so in its
// class or role, it has a background, or it is the only thing in a cell or
// box that has one.
func isButton(a *html.Node) bool {
	if styledAsButton(a) {
		return true
	}
	for p, depth := a.Parent, 0; p != nil && depth < 3; p, depth = p.Parent, depth+1 {
		if strings.TrimSpace(textOf(p)) != strings.TrimSpace(textOf(a)) {
			return false
		}
		if styledAsButton(p) || attr(p, "bgcolor") != "" {
			return true
		}
	}
	return false
}

func styledAsButton(n *html.Node) bool {
	if attr(n, "role") == "button" || reButtonClass.MatchString(attr(n, "class")) {
		return true
	}
	style := attr(n, "style")
	if reBackground.MatchString(style) && !strings.Contains(strings.ToLower(style), "transparent") {
		return true
	}
	return n.DataAtom == atom.A && reBorder.MatchString(style)
}

var (
	reColor    = regexp.MustCompile(`(?i)(?:^|;)\s*color\s*:\s*([^;]+)`)
	reBold     = regexp.MustCompile(`(?i)font-weight\s*:\s*(bold|[6-9]00)`)
	reBgColor  = regexp.MustCompile(`(?i)background(?:-color)?\s*:\s*(#[0-9a-f]{3}\b|#[0-9a-f]{6}\b)`)
	reLeftEdge = regexp.MustCompile(`(?i)border-left\s*:\s*([1-9][0-9]*)px\s+\w+\s+(#[0-9a-f]{3}\b|#[0-9a-f]{6}\b)`)
)

// textStyle reads the look of an element's text from its inline style:
// bold, or muted when it is a mid gray.
func textStyle(n *html.Node) (Style, bool) {
	style := attr(n, "style")
	if m := reColor.FindStringSubmatch(style); m != nil && gray(strings.TrimSpace(m[1])) {
		return Muted, true
	}
	if reBold.MatchString(style) {
		return Bold, true
	}
	return Plain, false
}

func gray(c string) bool {
	c = strings.ToLower(c)
	if c == "gray" || c == "grey" {
		return true
	}
	r, g, b, ok := parseHex(c)
	if !ok {
		return false
	}
	lo, hi := min(r, g, b), max(r, g, b)
	return hi-lo < 0x20 && lo >= 0x55 && hi <= 0xbb
}

func parseHex(c string) (r, g, b int, ok bool) {
	c = expandHex(c)
	if len(c) != 7 || c[0] != '#' {
		return 0, 0, 0, false
	}
	var v [3]int
	for i := range v {
		for _, ch := range c[1+2*i : 3+2*i] {
			d := strings.IndexRune("0123456789abcdef", ch)
			if d < 0 {
				return 0, 0, 0, false
			}
			v[i] = v[i]*16 + d
		}
	}
	return v[0], v[1], v[2], true
}

// expandHex turns #rgb into #rrggbb.
func expandHex(c string) string {
	c = strings.ToLower(c)
	if len(c) == 4 && c[0] == '#' {
		return string([]byte{'#', c[1], c[1], c[2], c[2], c[3], c[3]})
	}
	return c
}

// leftBorder returns the color of a visible left border, or "".
func leftBorder(n *html.Node) string {
	if m := reLeftEdge.FindStringSubmatch(attr(n, "style")); m != nil {
		return expandHex(m[2])
	}
	return ""
}

// buttonColor finds a button's background on it or the cell around it.
func buttonColor(a *html.Node) string {
	for n, depth := a, 0; n != nil && depth < 3; n, depth = n.Parent, depth+1 {
		if m := reBgColor.FindStringSubmatch(attr(n, "style")); m != nil {
			return expandHex(m[1])
		}
		if c := attr(n, "bgcolor"); strings.HasPrefix(c, "#") {
			return expandHex(c)
		}
	}
	return ""
}

var reHidden = regexp.MustCompile(`(?i)display\s*:\s*none|max-height\s*:\s*0|mso-hide\s*:\s*all`)

// hidden reports elements mail clients don't show, such as preheaders.
func hidden(n *html.Node) bool {
	return reHidden.MatchString(attr(n, "style")) || hasAttr(n, "hidden")
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
		if n.Type == html.ElementNode && (n.DataAtom == atom.Style || n.DataAtom == atom.Script || hidden(n)) {
			return
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return b.String()
}

// altOf returns the alt text of the images under n.
func altOf(n *html.Node) string {
	var parts []string
	var walk func(*html.Node)
	walk = func(n *html.Node) {
		if n.Type == html.ElementNode && n.DataAtom == atom.Img {
			if alt := attr(n, "alt"); alt != "" {
				parts = append(parts, alt)
			}
		}
		for c := n.FirstChild; c != nil; c = c.NextSibling {
			walk(c)
		}
	}
	walk(n)
	return strings.Join(parts, " ")
}
