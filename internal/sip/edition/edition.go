package edition

import (
	"slices"
	"strings"
	"time"

	"nmail/internal/sip/piece"
)

// longerMinutes is the reading time from which a piece counts as a longer read.
const longerMinutes = 3

// Ranker orders pieces for the front page; its first element is the lead.
type Ranker interface{ Rank([]piece.Piece) []piece.Piece }

// ByLength ranks newest first, then moves the piece with the most words
// (ties: the newest) to the front.
type ByLength struct{}

// Rank implements Ranker without touching its input.
func (ByLength) Rank(pieces []piece.Piece) []piece.Piece {
	out := slices.Clone(pieces)
	slices.SortFunc(out, func(a, b piece.Piece) int {
		if c := b.Arrived.Compare(a.Arrived); c != 0 {
			return c
		}
		return strings.Compare(a.ID, b.ID)
	})
	lead := 0
	for i, p := range out {
		if p.Words > out[lead].Words {
			lead = i
		}
	}
	if lead > 0 {
		top := out[lead]
		copy(out[1:lead+1], out[:lead])
		out[0] = top
	}
	return out
}

// Edition is one morning's paper.
type Edition struct {
	Cutoff time.Time
	Lead   *piece.Piece
	Longer []piece.Piece // minutes >= 3, after the lead
	Briefs []piece.Piece // the rest
}

// Compose builds the edition for cutoff, ranked ByLength.
func Compose(pieces []piece.Piece, cutoff time.Time) Edition {
	return ComposeWith(pieces, cutoff, ByLength{})
}

// ComposeWith keeps the pieces that arrived in [Start(cutoff), cutoff) and
// lays them out in the order r ranks them. pieces is not modified.
func ComposeWith(pieces []piece.Piece, cutoff time.Time, r Ranker) Edition {
	start := Start(cutoff)
	var in []piece.Piece
	for _, p := range pieces {
		if !p.Arrived.Before(start) && p.Arrived.Before(cutoff) {
			in = append(in, p)
		}
	}
	e := Edition{Cutoff: cutoff}
	ranked := r.Rank(in)
	if len(ranked) == 0 {
		return e
	}
	lead := ranked[0]
	e.Lead = &lead
	for _, p := range ranked[1:] {
		if p.Minutes() >= longerMinutes {
			e.Longer = append(e.Longer, p)
		} else {
			e.Briefs = append(e.Briefs, p)
		}
	}
	return e
}

// Order is the front-page and "next piece" order: lead, longer reads, briefs.
func (e Edition) Order() []piece.Piece {
	if e.Lead == nil {
		return nil
	}
	out := make([]piece.Piece, 0, e.Count())
	out = append(out, *e.Lead)
	out = append(out, e.Longer...)
	return append(out, e.Briefs...)
}

// Count is the number of pieces in the edition.
func (e Edition) Count() int {
	if e.Lead == nil {
		return 0
	}
	return 1 + len(e.Longer) + len(e.Briefs)
}

// Minutes is the total reading time.
func (e Edition) Minutes() int {
	total := 0
	for _, p := range e.Order() {
		total += p.Minutes()
	}
	return total
}

// Empty reports whether there is nothing to read.
func (e Edition) Empty() bool { return e.Lead == nil }
