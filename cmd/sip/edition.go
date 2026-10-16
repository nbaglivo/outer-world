package main

import (
	"fmt"
	"time"

	"nmail/internal/mailbox"
	"nmail/internal/sip/edition"
	"nmail/internal/sip/piece"
)

type windower interface {
	Window(from, to time.Time) ([]*mailbox.Message, error)
}

// buildEdition fetches the messages that arrived since the previous 06:30
// cutoff and composes this morning's edition.
func buildEdition(w windower, now time.Time, loc *time.Location) (edition.Edition, error) {
	cutoff := edition.Cutoff(now, loc)
	msgs, err := w.Window(edition.Start(cutoff), cutoff)
	if err != nil {
		return edition.Edition{}, fmt.Errorf("fetch the window: %w", err)
	}
	pieces := make([]piece.Piece, len(msgs))
	for i, m := range msgs {
		pieces[i] = piece.FromEmail(m)
	}
	return edition.Compose(pieces, cutoff), nil
}
