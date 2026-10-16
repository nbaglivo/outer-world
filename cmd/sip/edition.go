package main

import (
	"fmt"
	"io"
	"time"

	"nmail/internal/mailbox"
	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
)

type windower interface {
	Window(from, to time.Time) ([]*mailbox.Message, []error, error)
}

// buildEdition fetches the messages that arrived since the previous 06:30
// cutoff and composes this morning's edition. Messages that couldn't be read
// are left out with a line on warn.
func buildEdition(w windower, now time.Time, loc *time.Location, warn io.Writer) (edition.Edition, error) {
	cutoff := edition.Cutoff(now, loc)
	msgs, skipped, err := w.Window(edition.Start(cutoff), cutoff)
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
	return edition.Compose(pieces, cutoff), nil
}
