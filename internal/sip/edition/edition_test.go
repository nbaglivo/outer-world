package edition

import (
	"fmt"
	"slices"
	"testing"
	"time"

	"nmail/internal/sip/piece"
)

func madrid(t *testing.T) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	return loc
}

func ids(ps []piece.Piece) []string {
	var out []string
	for _, p := range ps {
		out = append(out, p.ID)
	}
	return out
}

func TestComposeWindow(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	start := Start(cutoff)
	pieces := []piece.Piece{
		{ID: "start", Arrived: start},
		{ID: "late", Arrived: cutoff.Add(-time.Second)},
		{ID: "cutoff", Arrived: cutoff},
		{ID: "early", Arrived: start.Add(-time.Second)},
	}
	if got := Compose(pieces, cutoff).Count(); got != 2 {
		t.Fatalf("Count() = %d, want 2", got)
	}
}

func TestComposeLead(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	at := func(h int) time.Time { return cutoff.Add(-time.Duration(h) * time.Hour) }
	pieces := []piece.Piece{
		{ID: "short", Words: 100, Arrived: at(1)},
		{ID: "old900", Words: 900, Arrived: at(5)},
		{ID: "new900", Words: 900, Arrived: at(3)},
	}
	e := Compose(pieces, cutoff)
	if e.Lead == nil || e.Lead.ID != "new900" {
		t.Fatalf("Lead = %+v, want new900", e.Lead)
	}
}

func TestComposeSplit(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	at := func(h int) time.Time { return cutoff.Add(-time.Duration(h) * time.Hour) }
	pieces := []piece.Piece{
		{ID: "lead", Words: 5000, Arrived: at(1)},
		{ID: "m2", Words: 460, Arrived: at(2)},
		{ID: "m3", Words: 690, Arrived: at(3)},
		{ID: "m5", Words: 1150, Arrived: at(4)},
	}
	e := Compose(pieces, cutoff)
	if got, want := ids(e.Longer), []string{"m3", "m5"}; !slices.Equal(got, want) {
		t.Errorf("Longer = %v, want %v", got, want)
	}
	if got, want := ids(e.Briefs), []string{"m2"}; !slices.Equal(got, want) {
		t.Errorf("Briefs = %v, want %v", got, want)
	}
	if got, want := ids(e.Order()), []string{"lead", "m3", "m5", "m2"}; !slices.Equal(got, want) {
		t.Errorf("Order = %v, want %v", got, want)
	}
	if got, want := e.Minutes(), 22+2+3+5; got != want {
		t.Errorf("Minutes = %d, want %d", got, want)
	}
}

func TestComposeEmpty(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	e := Compose(nil, cutoff)
	if !e.Empty() || e.Lead != nil || e.Count() != 0 || e.Minutes() != 0 {
		t.Fatalf("not empty: %+v", e)
	}
}

type reverse struct{}

func (reverse) Rank(ps []piece.Piece) []piece.Piece {
	out := slices.Clone(ps)
	slices.Reverse(out)
	return out
}

func TestComposeWithRanker(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	pieces := []piece.Piece{
		{ID: "a", Words: 900, Arrived: cutoff.Add(-1 * time.Hour)},
		{ID: "b", Words: 100, Arrived: cutoff.Add(-2 * time.Hour)},
		{ID: "c", Words: 500, Arrived: cutoff.Add(-3 * time.Hour)},
	}
	e := ComposeWith(pieces, cutoff, reverse{})
	if e.Lead == nil || e.Lead.ID != "c" {
		t.Fatalf("Lead = %+v, want c", e.Lead)
	}
}

func TestComposeDeterministic(t *testing.T) {
	cutoff := time.Date(2026, 10, 16, 6, 30, 0, 0, madrid(t))
	at := cutoff.Add(-time.Hour)
	var pieces []piece.Piece
	for i := range 6 {
		pieces = append(pieces, piece.Piece{ID: fmt.Sprint("p", i), Words: 300, Arrived: at})
	}
	orig := ids(pieces)
	want := ids(Compose(pieces, cutoff).Order())
	slices.Reverse(pieces)
	if got := ids(Compose(pieces, cutoff).Order()); !slices.Equal(got, want) {
		t.Errorf("order depends on input order: %v vs %v", got, want)
	}
	slices.Reverse(pieces)
	Compose(pieces, cutoff)
	if !slices.Equal(ids(pieces), orig) {
		t.Errorf("Compose mutated its input")
	}
}
