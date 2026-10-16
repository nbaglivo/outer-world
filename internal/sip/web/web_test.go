package web

import (
	"flag"
	"html/template"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
)

var update = flag.Bool("update", false, "rewrite golden files")

func madrid(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func fixture(t *testing.T) (edition.Edition, *time.Location) {
	t.Helper()
	loc := madrid(t)
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, loc)
	at := func(h, m int) time.Time { return time.Date(2026, 10, 15, h, m, 0, 0, loc) }
	lead := piece.Piece{
		ID: "lead", Title: "The quiet return of the personal website",
		Dek:    "Why the next decade of the web might look a lot like 2004, only with better typography.",
		Author: "Ben Thompson", Source: "Stratechery", Channel: "email",
		Published: at(18, 2), Arrived: at(18, 3),
		HTML:     template.HTML("<p>There was a time when having a website meant something specific.</p><p>Somewhere along the way we traded that for feeds.</p>"),
		Words:    2700,
		Original: "https://stratechery.com/2026/personal-website",
	}
	return edition.Edition{
		Cutoff: cutoff,
		Lead:   &lead,
		Longer: []piece.Piece{
			{
				ID: "slow", Title: "Notes on slow software", Dek: "On tools that refuse to hurry you.",
				Author: "Robin Sloan", Source: "Year of the Meteor", Channel: "email",
				Published: at(9, 15), Arrived: at(9, 16),
				HTML:     template.HTML(`<p>Some tools <em>refuse</em> to hurry you.</p><img src="https://example.com/desk.jpg" alt="a desk">`),
				Words:    1380,
				Original: "https://robinsloan.com/slow",
			},
			{
				ID: "dns", Title: "How DNS caching actually works", Dek: "A zine draft, with drawings.",
				Author: "Julia Evans", Source: "Wizard Zines", Channel: "email",
				Published: at(7, 40), Arrived: at(7, 41),
				HTML:  template.HTML("<p>Every resolver keeps a cache.</p>"),
				Words: 1840,
			},
		},
		Briefs: []piece.Piece{
			{
				ID: "models", Author: "Simon Willison", Source: "simonwillison.net", Channel: "email",
				Text:      "Wrote up my notes on running small models locally — the surprising part was the memory, not the speed. I tried four of them on a laptop with sixteen gigabytes and the results were not what I expected at all, so here is the whole table with every number I measured along the way and what I make of it.",
				Published: at(22, 10), Arrived: at(22, 11),
				HTML:  template.HTML("<p>Wrote up my notes.</p>"),
				Words: 60,
			},
			{
				ID: "walk", Dek: "Walked 40km today. The rice is almost ready.",
				Author: "Craig Mod", Source: "Ridgeline", Channel: "email",
				Published: at(20, 0), Arrived: at(20, 1),
				HTML:  template.HTML("<p>Walked 40km today.</p>"),
				Words: 120,
			},
			{
				ID: "go", Text: "Go 1.27.3 is out with a fix for the race detector on arm64.",
				Author: "Go Team", Source: "golang-announce", Channel: "email",
				Published: at(19, 30), Arrived: at(19, 31),
				HTML:  template.HTML("<p>Go 1.27.3 is out.</p>"),
				Words: 14,
			},
		},
	}, loc
}

func get(t *testing.T, h http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func golden(t *testing.T, name string, got string) {
	t.Helper()
	path := filepath.Join("testdata", name)
	if *update {
		if err := os.WriteFile(path, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if got != string(want) {
		t.Errorf("%s differs from golden; run with -update and review the diff\n--- got ---\n%s", name, got)
	}
}

func TestFrontGolden(t *testing.T) {
	ed, loc := fixture(t)
	rec := get(t, Handler(ed, loc), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	golden(t, "front.golden.html", rec.Body.String())
}

func TestPieceGolden(t *testing.T) {
	ed, loc := fixture(t)
	rec := get(t, Handler(ed, loc), "/p/2")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /p/2 = %d, want 200", rec.Code)
	}
	golden(t, "piece.golden.html", rec.Body.String())
}

func TestEmptyGolden(t *testing.T) {
	loc := madrid(t)
	ed := edition.Edition{Cutoff: time.Date(2026, 10, 16, 6, 30, 0, 0, loc)}
	rec := get(t, Handler(ed, loc), "/")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET / = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Nothing new since yesterday morning. Enjoy the coffee.") {
		t.Errorf("empty front page lacks the empty copy")
	}
	golden(t, "empty.golden.html", body)
}

func TestFrontHasNoSources(t *testing.T) {
	ed, loc := fixture(t)
	body := get(t, Handler(ed, loc), "/").Body.String()
	for _, p := range ed.Order() {
		for _, s := range []string{p.Source, p.Author} {
			if strings.Contains(body, s) {
				t.Errorf("front page contains %q", s)
			}
		}
	}
}

func TestPieceNotFound(t *testing.T) {
	ed, loc := fixture(t)
	h := Handler(ed, loc)
	for _, path := range []string{"/p/0", "/p/7", "/p/x"} {
		if code := get(t, h, path).Code; code != http.StatusNotFound {
			t.Errorf("GET %s = %d, want 404", path, code)
		}
	}
}

func TestNextLink(t *testing.T) {
	ed, loc := fixture(t)
	h := Handler(ed, loc)
	if body := get(t, h, "/p/6").Body.String(); strings.Contains(body, "next piece") {
		t.Errorf("last piece has a next link")
	}
	if body := get(t, h, "/p/1").Body.String(); !strings.Contains(body, `href="/p/2"`) {
		t.Errorf("/p/1 does not link /p/2")
	}
}

func TestAssets(t *testing.T) {
	ed, loc := fixture(t)
	h := Handler(ed, loc)
	css := get(t, h, "/assets/style.css")
	if css.Code != http.StatusOK {
		t.Fatalf("GET /assets/style.css = %d, want 200", css.Code)
	}
	if ct := css.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/css") {
		t.Errorf("style.css Content-Type = %q, want text/css", ct)
	}
	if code := get(t, h, "/assets/fonts/newsreader-300.woff2").Code; code != http.StatusOK {
		t.Errorf("GET newsreader-300.woff2 = %d, want 200", code)
	}
}

func TestNoGoogleFonts(t *testing.T) {
	ed, loc := fixture(t)
	h := Handler(ed, loc)
	for _, path := range []string{"/", "/p/1", "/p/4", "/assets/style.css", "/assets/mark.svg"} {
		body, _ := io.ReadAll(get(t, h, path).Body)
		if strings.Contains(string(body), "fonts.googleapis.com") {
			t.Errorf("%s references fonts.googleapis.com", path)
		}
	}
}
