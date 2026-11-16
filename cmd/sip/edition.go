package main

import (
	"fmt"
	"io"
	"time"

	"nmail/internal/mailbox"
	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
	"nmail/internal/sip/stw"
)

// fediverse fetches the fediverse items received in (since, until].
type fediverse func(since, until time.Time) ([]stw.Item, error)

type windower interface {
	Window(from, to time.Time) ([]*mailbox.Message, []error, error)
}

// buildEdition fetches the messages that arrived since the previous 06:30
// cutoff and composes this morning's edition. Messages that couldn't be read
// are left out with a line on warn. A missing or failing fediverse source only
// writes a warning; the edition is then made from email alone.
func buildEdition(w windower, fedi fediverse, now time.Time, loc *time.Location, warn io.Writer) (edition.Edition, error) {
	cutoff := edition.Cutoff(now, loc)
	start := edition.Start(cutoff)
	msgs, skipped, err := w.Window(start, cutoff)
	if err != nil {
		return edition.Edition{}, fmt.Errorf("fetch the window: %w", err)
	}
	for _, err := range skipped {
		fmt.Fprintf(warn, "sip: skipped %v\n", err)
	}
	pieces := make([]piece.Piece, len(msgs))
	for i, m := range msgs {
		pieces[i] = piece.FromEmail(m)
	}
	if fedi == nil {
		fmt.Fprintln(warn, "sip: fediverse skipped: run sip login fediverse")
	} else if items, err := fedi(start, cutoff); err != nil {
		fmt.Fprintf(warn, "sip: fediverse: %v\n", err)
	} else {
		for _, it := range items {
			pieces = append(pieces, piece.FromItem(it))
		}
	}
	return edition.Compose(pieces, cutoff), nil
}
