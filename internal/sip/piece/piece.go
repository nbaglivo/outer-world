// Package piece is one readable item in the paper: a newsletter or a
// message, cleaned for the web.
package piece

import (
	"html/template"
	"time"
)

// wordsPerMinute is the reading speed behind Minutes.
const wordsPerMinute = 230

// Piece is one newsletter, ready to show.
type Piece struct {
	ID, Title, Dek, Author, Source, Channel string
	Published, Arrived                      time.Time
	HTML                                    template.HTML // cleaned, safe to emit
	Text                                    string
	Words                                   int
	Original                                string // http(s) URL or ""
	Raw                                     []byte
}

// Minutes is the reading time: at least one.
func (p Piece) Minutes() int {
	return max(1, (p.Words+wordsPerMinute-1)/wordsPerMinute)
}

// Headline is what to call the piece on a page.
func (p Piece) Headline() string {
	switch {
	case p.Title != "":
		return p.Title
	case p.Dek != "":
		return p.Dek
	}
	return "Untitled"
}
