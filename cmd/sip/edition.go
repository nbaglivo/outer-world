package main

import (
	"context"
	"fmt"
	"io"
	"time"

	"nmail/internal/mailbox"
	"nmail/internal/sip/edition"
	"nmail/internal/sip/mastodon"
	"nmail/internal/sip/piece"
)

type windower interface {
	Window(from, to time.Time) ([]*mailbox.Message, []error, error)
}

// feed is a source of pieces besides email. A nil fetch means no saved token.
type feed struct {
	name  string
	fetch func(since, until time.Time, note func(string)) ([]piece.Piece, error)
}

// buildEdition fetches the messages that arrived since the previous 06:30
// cutoff and composes this morning's edition. Messages that couldn't be read
// are left out with a line on warn. A feed that fails is left out the same
// way; only an email failure is fatal.
func buildEdition(w windower, feeds []feed, now time.Time, loc *time.Location, warn io.Writer) (edition.Edition, error) {
	cutoff := edition.Cutoff(now, loc)
	since := edition.Start(cutoff)
	msgs, skipped, err := w.Window(since, cutoff)
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
	for _, f := range feeds {
		if f.fetch == nil {
			fmt.Fprintf(warn, "sip: %s skipped: run sip login %s\n", f.name, f.name)
			continue
		}
		note := func(text string) { fmt.Fprintf(warn, "sip: %s: %s\n", f.name, text) }
		ps, err := f.fetch(since, cutoff, note)
		if err != nil {
			fmt.Fprintf(warn, "sip: %s: %v\n", f.name, err)
			continue
		}
		pieces = append(pieces, ps...)
	}
	return edition.Compose(pieces, cutoff), nil
}

// mastodonFeed reads the home timeline. Compose filters by the window, so the
// until bound is unused.
func mastodonFeed(home func(ctx context.Context, since time.Time) (mastodon.Timeline, error), loc *time.Location) feed {
	return feed{name: "mastodon", fetch: func(since, _ time.Time, note func(string)) ([]piece.Piece, error) {
		tl, err := home(context.Background(), since)
		if err != nil {
			return nil, err
		}
		ps := make([]piece.Piece, len(tl.Statuses))
		for i, s := range tl.Statuses {
			ps[i] = piece.FromStatus(s)
		}
		if !tl.Complete && !tl.Oldest.IsZero() {
			note("timeline only reaches back to " + tl.Oldest.In(loc).Format(time.RFC3339))
		}
		return ps, nil
	}}
}
