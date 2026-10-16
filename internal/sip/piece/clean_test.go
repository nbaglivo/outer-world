package piece

import (
	"strings"
	"testing"
)

func TestCleanStripsNoise(t *testing.T) {
	c := Clean(`<html><head><style>p{}</style><script>x()</script></head><body>
<div style="display:none">Inside: three essays on slow software</div>
<p><a href="https://ex.com/web">View in browser</a></p>
<h1 class="h" style="color:red" onclick="x()">Notes</h1>
<p>Read <a href="https://ex.com/a">this</a> and <a href="javascript:alert(1)">that</a>.</p>
<img src="https://ex.com/pic.jpg" alt="A cup"><img src="https://t.ex.com/o.gif" width="1" height="1">
<table><tr><td><p>Cell text</p></td></tr></table></body></html>`)

	if c.Preheader != "Inside: three essays on slow software" {
		t.Errorf("Preheader = %q", c.Preheader)
	}
	if c.ViewLink != "https://ex.com/web" {
		t.Errorf("ViewLink = %q", c.ViewLink)
	}
	out := string(c.HTML)
	for _, want := range []string{`<h1>Notes</h1>`, `href="https://ex.com/a"`, `src="https://ex.com/pic.jpg"`, `alt="A cup"`, "Cell text"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML lacks %q:\n%s", want, out)
		}
	}
	for _, bad := range []string{"<script", "<style", "style=", "class=", "onclick", "javascript:", "o.gif", "<table", "<td", "three essays", "View in browser"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML contains %q:\n%s", bad, out)
		}
	}
}

func TestCleanViewLinkInsideSentenceKeepsText(t *testing.T) {
	c := Clean(`<p>Having trouble? <a href="https://ex.com/v">View online</a> or reply.</p><p>Body</p>`)
	if c.ViewLink != "https://ex.com/v" {
		t.Errorf("ViewLink = %q", c.ViewLink)
	}
	out := string(c.HTML)
	if strings.Contains(out, "View online") || !strings.Contains(out, "Having trouble?") {
		t.Errorf("HTML = %s", out)
	}
}

func TestCleanViewLinkIgnoresNonHTTP(t *testing.T) {
	c := Clean(`<p><a href="javascript:x()">View in browser</a></p>`)
	if c.ViewLink != "" {
		t.Errorf("ViewLink = %q", c.ViewLink)
	}
}

func TestCleanDropsHiddenVariants(t *testing.T) {
	c := Clean(`<p style="font-size:0">a-zero</p><p style="visibility:hidden">b-vis</p><p hidden>c-attr</p><p style="max-height:0;overflow:hidden">d-max</p><p>kept</p>`)
	out := string(c.HTML)
	for _, bad := range []string{"a-zero", "b-vis", "c-attr", "d-max"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML contains %q: %s", bad, out)
		}
	}
	if !strings.Contains(out, "kept") {
		t.Errorf("HTML = %s", out)
	}
}

func TestCleanLinksOpenSafely(t *testing.T) {
	out := string(Clean(`<a href="https://ex.com/a">x</a>`).HTML)
	if !strings.Contains(out, `target="_blank"`) || !strings.Contains(out, "noreferrer") {
		t.Errorf("HTML = %s", out)
	}
}

func TestCleanDropsPixelsSizedByStyleOrDecimals(t *testing.T) {
	out := string(Clean(`<p>x</p>` +
		`<img src="https://t.ex.com/a.gif" style="width:1px;height:1px">` +
		`<img src="https://t.ex.com/b.gif" style="WIDTH: 0 ; border:0">` +
		`<img src="https://t.ex.com/c.gif" width="1.0" height="1.0">` +
		`<img src="https://t.ex.com/d.gif" height="2.5px">` +
		`<img src="https://ex.com/big.jpg" style="width:600px" width="600.0">`).HTML)
	for _, bad := range []string{"a.gif", "b.gif", "c.gif"} {
		if strings.Contains(out, bad) {
			t.Errorf("HTML keeps pixel %s: %s", bad, out)
		}
	}
	for _, want := range []string{"d.gif", "big.jpg"} {
		if !strings.Contains(out, want) {
			t.Errorf("HTML lacks %s: %s", want, out)
		}
	}
}

func TestCleanDropsEveryViewLink(t *testing.T) {
	c := Clean(`<p><a href="https://ex.com/top">View in browser</a></p><p>Body</p>` +
		`<p><a href="https://ex.com/bottom">View online</a></p>`)
	if c.ViewLink != "https://ex.com/top" {
		t.Errorf("ViewLink = %q", c.ViewLink)
	}
	out := string(c.HTML)
	if strings.Contains(out, "View") || strings.Contains(out, "ex.com/bottom") || !strings.Contains(out, "Body") {
		t.Errorf("HTML = %s", out)
	}
}
