package render

import (
	"regexp"
	"strings"
)

var reURL = regexp.MustCompile(`(?i)\b(https?://|mailto:)[^\s<>"]+`)

// Text renders a plain-text body, turning bare URLs into links.
func Text(src string) Doc {
	var d Doc
	for _, raw := range strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n") {
		raw = strings.ReplaceAll(strings.TrimRight(raw, " \t"), "\t", "    ")
		line := Line{Indent: len(raw) - len(strings.TrimLeft(raw, " "))}
		raw = raw[line.Indent:]
		last := 0
		for _, m := range reURL.FindAllStringIndex(raw, -1) {
			url := strings.TrimRight(raw[m[0]:m[1]], ".,;:!?)]}'")
			end := m[0] + len(url)
			line.Spans = appendPlain(line.Spans, raw[last:m[0]])
			d.Links = append(d.Links, Link{URL: url, Text: url})
			line.Spans = append(line.Spans, Span{Text: url, Style: LinkText, Link: len(d.Links) - 1})
			last = end
		}
		line.Spans = appendPlain(line.Spans, raw[last:])
		d.Lines = append(d.Lines, line)
	}
	return d
}

func appendPlain(spans []Span, s string) []Span {
	if s == "" {
		return spans
	}
	return append(spans, Span{Text: s, Style: Plain, Link: -1})
}
