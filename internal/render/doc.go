// Package render turns a message into styled terminal text with links the
// reader can step through. Rules pick a renderer per message.
package render

import (
	"strings"

	"github.com/charmbracelet/lipgloss"
)

type Style int

const (
	Plain Style = iota
	Bold
	Italic
	Heading
	Muted
	LinkText
	ButtonText
	HRule // a horizontal rule; its line is drawn across the full width
)

type Link struct {
	URL    string
	Text   string
	Button bool
	Color  string // a button's background, "" for the default
}

// Span is a run of text in one style, part of Link (an index into
// Doc.Links) unless Link is -1.
type Span struct {
	Text  string
	Style Style
	Link  int
}

// Line is a logical line, wrapped to the screen width at display time.
// Every row starts Indent columns in, continuations Hang more. Lines with
// a Bar color get a bar down their left edge, like a card or a quote.
type Line struct {
	Spans  []Span
	Indent int
	Hang   int
	Bar    string
}

type Doc struct {
	Lines []Line
	Links []Link
}

var (
	styles = map[Style]lipgloss.Style{
		Plain:      lipgloss.NewStyle(),
		Bold:       lipgloss.NewStyle().Bold(true),
		Italic:     lipgloss.NewStyle().Italic(true),
		Heading:    lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212")),
		Muted:      lipgloss.NewStyle().Foreground(lipgloss.Color("245")),
		LinkText:   lipgloss.NewStyle().Underline(true).Foreground(lipgloss.Color("39")),
		ButtonText: lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("62")),
		HRule:      lipgloss.NewStyle().Foreground(lipgloss.Color("240")),
	}
	focusLink   = lipgloss.NewStyle().Reverse(true)
	focusButton = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("230")).Background(lipgloss.Color("212"))
)

// Render wraps d to width and styles it, highlighting link focus (-1 for
// none). It also returns the line each link starts on.
func (d Doc) Render(width, focus int) (string, []int) {
	width = max(width, 10)
	starts := make([]int, len(d.Links))
	for i := range starts {
		starts[i] = -1
	}
	var out []string
	for _, l := range d.Lines {
		w := width
		if l.Bar != "" {
			w = width - 2
		}
		for _, row := range wrap(l, w) {
			var b strings.Builder
			if l.Bar != "" {
				bar := "▌"
				if len(row) > 0 {
					bar += " "
				}
				b.WriteString(lipgloss.NewStyle().Foreground(lipgloss.Color(l.Bar)).Render(bar))
			}
			for _, s := range row {
				if s.Link >= 0 && starts[s.Link] < 0 {
					starts[s.Link] = len(out)
				}
				b.WriteString(d.styleSpan(s, w, focus))
			}
			out = append(out, b.String())
		}
	}
	return strings.Join(out, "\n"), starts
}

func (d Doc) styleSpan(s Span, width, focus int) string {
	if s.Style == HRule {
		return styles[HRule].Render(strings.Repeat("─", width))
	}
	st := styles[s.Style]
	if s.Style == ButtonText && d.Links[s.Link].Color != "" {
		st = st.Background(lipgloss.Color(d.Links[s.Link].Color))
	}
	if s.Link == focus && s.Link >= 0 {
		st = focusLink
		if d.Links[s.Link].Button {
			st = focusButton
		}
	}
	text := st.Render(s.Text)
	if s.Link >= 0 {
		// OSC 8 makes the link clickable in terminals that support it.
		text = "\x1b]8;;" + d.Links[s.Link].URL + "\x1b\\" + text + "\x1b]8;;\x1b\\"
	}
	return text
}

// wrap breaks l into rows no wider than width. Buttons never break.
func wrap(l Line, width int) [][]Span {
	var rows [][]Span
	var row []Span
	col, indent := 0, 0
	startRow := func(n int) {
		indent = min(n, width/2)
		row, col = nil, indent
		if indent > 0 {
			row = []Span{{Text: strings.Repeat(" ", indent), Style: Plain, Link: -1}}
		}
	}
	newRow := func() {
		rows = append(rows, trimTrailingSpace(row))
		startRow(l.Indent + l.Hang)
	}
	startRow(l.Indent)
	add := func(s Span, text string) {
		if n := len(row); n > 0 && row[n-1].Style == s.Style && row[n-1].Link == s.Link {
			row[n-1].Text += text
		} else {
			s.Text = text
			row = append(row, s)
		}
		col += lipgloss.Width(text)
	}

	for _, s := range l.Spans {
		if s.Style == HRule {
			if col > indent {
				newRow()
			}
			row = append(row, s)
			col = width
			continue
		}
		tokens := splitWords(s.Text)
		if s.Style == ButtonText {
			tokens = []string{s.Text}
		}
		for _, tok := range tokens {
			w := lipgloss.Width(tok)
			if tok == " " {
				if col > indent && col < width {
					add(s, tok)
				}
				continue
			}
			if col+w > width && col > indent {
				newRow()
			}
			for col+w > width {
				// Longer than a whole row: hard-cut it.
				head, tail := cut(tok, width-col)
				add(s, head)
				newRow()
				tok, w = tail, lipgloss.Width(tail)
			}
			add(s, tok)
		}
	}
	rows = append(rows, trimTrailingSpace(row))
	return rows
}

func trimTrailingSpace(row []Span) []Span {
	for len(row) > 0 {
		last := &row[len(row)-1]
		if last.Style == ButtonText || last.Style == HRule {
			return row
		}
		last.Text = strings.TrimRight(last.Text, " ")
		if last.Text != "" {
			return row
		}
		row = row[:len(row)-1]
	}
	return row
}

// splitWords splits s into words and single-space separators.
func splitWords(s string) []string {
	var out []string
	for i, w := range strings.Split(s, " ") {
		if i > 0 {
			out = append(out, " ")
		}
		if w != "" {
			out = append(out, w)
		}
	}
	return out
}

// cut splits s after at most n display columns (at least one rune).
func cut(s string, n int) (string, string) {
	col := 0
	for i, r := range s {
		w := lipgloss.Width(string(r))
		if col+w > n && i > 0 {
			return s[:i], s[i:]
		}
		col += w
	}
	return s, ""
}
