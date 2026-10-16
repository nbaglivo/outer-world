// Package web serves the edition as HTML pages: the front page, one page
// per piece, and the embedded stylesheet, mark and fonts.
package web

import (
	"bytes"
	"embed"
	"html/template"
	"io/fs"
	"net/http"
	"strconv"
	"strings"
	"time"

	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
)

// briefChars is roughly how much of a brief the front page shows.
const briefChars = 280

// csp lets pages load remote images but nothing else from elsewhere, and run
// no script at all: piece bodies are newsletter HTML.
const csp = "default-src 'none'; img-src http: https: data:; style-src 'self'; font-src 'self'"

//go:embed templates assets
var files embed.FS

// item is a piece with its 1-based place in the edition.
type item struct {
	N int
	piece.Piece
}

type frontPage struct {
	Edition        edition.Edition
	Lead           item
	Longer, Briefs []item
}

type piecePage struct {
	item
	Next int // 0 when this is the last piece
}

// Handler serves ed with times shown in loc.
func Handler(ed edition.Edition, loc *time.Location) http.Handler {
	funcs := template.FuncMap{
		"longdate": func(t time.Time) string { return t.In(loc).Format("Monday, 2 January 2006") },
		"stamp":    func(t time.Time) string { return t.In(loc).Format("Mon 2 Jan, 15:04") },
		"excerpt":  excerpt,
	}
	front := parse(funcs, "front.html")
	page := parse(funcs, "piece.html")

	order := ed.Order()
	items := make([]item, len(order))
	for i, p := range order {
		items[i] = item{N: i + 1, Piece: p}
	}
	fp := frontPage{Edition: ed}
	if len(items) > 0 {
		fp.Lead = items[0]
		fp.Longer = items[1 : 1+len(ed.Longer)]
		fp.Briefs = items[1+len(ed.Longer):]
	}

	assets, err := fs.Sub(files, "assets")
	if err != nil {
		panic(err)
	}

	mux := http.NewServeMux()
	mux.HandleFunc("GET /{$}", func(w http.ResponseWriter, r *http.Request) {
		render(w, front, fp)
	})
	mux.HandleFunc("GET /p/{n}", func(w http.ResponseWriter, r *http.Request) {
		n, err := strconv.Atoi(r.PathValue("n"))
		if err != nil || n < 1 || n > len(items) {
			http.NotFound(w, r)
			return
		}
		pp := piecePage{item: items[n-1]}
		if n < len(items) {
			pp.Next = n + 1
		}
		render(w, page, pp)
	})
	mux.Handle("GET /assets/", http.StripPrefix("/assets/", http.FileServerFS(assets)))
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Security-Policy", csp)
		mux.ServeHTTP(w, r)
	})
}

func parse(funcs template.FuncMap, name string) *template.Template {
	return template.Must(template.New("layout.html").Funcs(funcs).
		ParseFS(files, "templates/layout.html", "templates/"+name))
}

func render(w http.ResponseWriter, t *template.Template, data any) {
	var buf bytes.Buffer
	if err := t.Execute(&buf, data); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	buf.WriteTo(w)
}

// excerpt is the start of a brief's text, or its dek when it has none, cut
// on a word boundary.
func excerpt(p piece.Piece) string {
	s := p.Text
	if strings.TrimSpace(s) == "" {
		s = p.Dek
	}
	s = strings.Join(strings.Fields(s), " ")
	if len([]rune(s)) <= briefChars {
		return s
	}
	cut := string([]rune(s)[:briefChars])
	if i := strings.LastIndex(cut, " "); i > 0 {
		cut = cut[:i]
	}
	return strings.TrimRight(cut, " ,;:.—–-") + "…"
}
