# One sip at a time: design

A morning paper made from the things you subscribed to. Every day it composes
one edition from the last 24 hours of newsletters (and, later, fediverse posts
from accounts you follow) and lays it out like a calm broadsheet you read
while drinking your coffee.

It is a second app in the nmail repository. Both apps share the code that
talks to IMAP and the keychain.

## Intent

The user opens it in the morning and just starts reading. They trust the
content because they subscribed to all of it; curation (AI, later) will
improve that trust over time, but v1 has none. Success is that opening it
feels like picking up a newspaper: a front page to scan, pieces to read in
full, and a sense of being done at the end.

## Goals

- One edition per day, **built at 06:30 local time** and the same all day.
  Its window is **yesterday 06:30 to today 06:30** (local), even if a day was
  skipped. Before 06:30, the current edition is still yesterday's.
- A broadsheet front page that arranges pieces **by their shape, not their
  source**: no source labels on the front page.
- Opening a piece shows it in full with a byline: author, source, channel,
  time, reading length.
- The calm look of the user's blog, made slightly more editorial.
- Three surfaces for the same edition, eventually: terminal, native window,
  PDF.
- A monorepo where nmail and this app share mail and keychain code.

## Non-goals (v1)

- AI curation or ranking. Leave a seam for it (see Composition), build none.
- RSS. Later.
- Writing to the mailbox: no marking seen, no deleting, no moving.
- Read/unread tracking inside the app.
- Archive of past editions.

## Anti-goals

- Feeling like an inbox or a feed: no unread counts, no infinite scroll, no
  per-source grouping on the front page.
- Noise: tracking pixels, "view in browser" chrome and unsubscribe footers
  rendered as if they were content are the main way this could fail while
  technically working. Strip what can be stripped safely.
- Changing nmail's behaviour while restructuring the repo.

## Constraints

- Go (the repo's language), runtimes via mise (`mise.toml` pins `go`).
- Conventional commits; config files comment-free.
- IMAP access is read-only (`Select` with `ReadOnly: true`, `Peek` fetches),
  so reading an edition never marks mail as seen.

## Name and mark

- Name: **One sip at a time**. Binary and command: `sip` (my call).
- Mark: a fine single-line drawing of a profile sipping from a cup, eye
  closed, one wisp of steam ("B4"). Stroke-only SVG, `stroke-linecap` and
  `stroke-linejoin` round, `currentColor`, viewBox `0 0 64 64`:

```svg
<svg viewBox="0 0 64 64" fill="none" stroke="currentColor" stroke-width="1.25" stroke-linecap="round" stroke-linejoin="round"><g transform="translate(-4 0)"><path d="M18 56c0-5-3-8-4-13-2-8-1-17 5-24 5-6 14-9 21-6 4 2 5 6 5 10l3 6c.5 1 0 1.6-1 1.6h-2"/><path d="M43 39c0 2-1 3-3 3.5-2 .5-3 2-3 4v9"/><g transform="rotate(-28 45 31)"><path d="M45 31h13v6.5c0 4-3 6.5-6.5 6.5S45 41.5 45 37.5z"/><path d="M58 33c2.6 0 3.8 1.3 3.8 2.8S60.6 38.6 58 38.6"/></g><path d="M54 19c-1.2-2.2 1.2-3.3 0-5.5"/><path d="M37 24.5c1 1 2.4 1 3.4 0"/></g></svg>
```

  At small sizes (≤ 24 px) use a stroke width of about 2.6 so it stays
  visible.

## Sources

### Email (v1)

- One dedicated mailbox, **subs@nbaglivo.dev**, that receives only
  subscriptions. Every message in its INBOX inside the window is part of the
  edition; there is no newsletter detection.
- IMAP host, port and user from flags/env like nmail (`SIP_HOST`,
  `SIP_PORT` default 993, `SIP_USER`); password from the keychain.
- Window query: IMAP `SEARCH SINCE <date of window start>` (day granularity),
  then filter on the message's internal date to the exact window. Fetch
  full bodies with `Peek`.

### Fediverse (next milestone, not in the first build)

- From the user's warmhole server (`https://ap.nbaglivo.dev`), reader API
  `GET /reader/v1/sites/inner-monologue/items` with `since` = window start and `until` = cutoff,
  `order=asc`, paging with `next_cursor`, bearer reader token from the
  keychain.
- The contract is warmhole's
  [`docs/reader-api.openapi.json`](../../../../warmhole/docs/reader-api.openapi.json)
  (repo `nbaglivo/warmhole`); design in its
  `docs/superpowers/specs/2026-10-16-reader-api-design.md`. warmhole doesn't
  implement it yet; the client is built and tested against the contract with
  a fake server serving the contract's examples.

## The piece

Every source becomes the same value; everything downstream only knows
pieces.

| Field | Email | Fediverse item |
|---|---|---|
| `ID` | Message-ID header (fallback: UID) | `id` |
| `Title` | Subject, with leading list tags like `[Weekly]` removed | `title`; null for notes |
| `Dek` (one-line summary) | Preheader text if present, else first sentence of the body, ≤ 160 chars | `summary`, else first sentence of `content_text` |
| `Author` | From display name, else address | `author.name`, else `author.handle` |
| `Source` | `List-Id` description, else From display name, else From domain | author's server host |
| `Channel` | `email` | `fediverse` |
| `Published` (shown in the byline) | Date header, else internal date | `published` |
| `Arrived` (decides the window) | IMAP internal date | `received_at` |
| `HTML` | cleaned body HTML (see Cleaning) | `content_html` (already sanitized by warmhole) |
| `Text` | text/plain part, else text derived from HTML | `content_text` |
| `Words` / `Minutes` | word count of `Text`; minutes = ceil(words / 230), min 1 | same |
| `Original` | `List-Post`/"view in browser" link if found, else none | `url` |
| `Raw` | the RFC 822 bytes (for "open in nmail/mail app") | none |

Boosts show "boosted by …" in the byline.

### Cleaning email HTML

Allowlist sanitizer on the server side of the app (Go, e.g.
`github.com/microcosm-cc/bluemonday`): keep text structure (headings,
paragraphs, lists, blockquote, pre/code, emphasis, links, images, simple
tables used for layout get unwrapped to their cell content). Drop scripts,
styles, inline `style`/`class`, tracking pixels (images with width or height
≤ 2, or hidden), and remote fonts. Remote images are kept and loaded (user's
call can revisit; default is load, since newsletters are mostly images-with-
captions). Footers are not detected in v1.

The terminal surface keeps using nmail's `render` package (rules included),
which already turns email HTML into terminal text with navigable links.

## Composition

The edition is a pure function `Compose(pieces, cutoff) Edition`, where
`cutoff` is the most recent 06:30 local at or before the time `sip` runs
(`edition.Cutoff(now, loc)`; handle DST by computing 06:30 on the calendar
day, so a window can be 23 or 25 hours). Stateless by design (my call): every
run on the same day, browser or PDF, gives the same edition without storing
anything, as long as the mailbox isn't changed.

1. Keep pieces whose `Arrived` falls in `[previous cutoff, cutoff)`. Arrival, not
   the sender's Date header, decides membership (my call): newsletters are
   often queued, and a skipped day must still show exactly what arrived.
2. Order by `Arrived`, newest first.
3. **Lead**: the piece with the most words (ties: newest). My call, until
   curation exists.
4. **Longer reads**: remaining pieces with `Minutes ≥ 3`.
5. **Briefs**: everything else.
6. Masthead numbers: piece count and the sum of minutes.

A `Ranker` interface (`Rank([]Piece) []Piece`) wraps steps 2–3 so curation
can replace them later; v1 ships one implementation that does exactly the
above.

With no pieces, the edition says so quietly ("Nothing new since yesterday
morning. Enjoy the coffee.").

## Surfaces

All surfaces render the same `Edition` value.

### Visual design (shared by HTML, native and PDF)

From the blog, `innermonologue.nbaglivo.dev`:

- paper `#faf8f4`, ink `#38352f`, headline ink `#201e1a`, muted `#6f6a60`,
  meta `#756e60` / `#8a857b`, hairlines `#ebe6dc`;
- metadata (dates, bylines, reading time, kickers) in IBM Plex Mono, small,
  kickers uppercase with letter-spacing;
- my addition for the editorial feel: Newsreader (serif) for headlines and
  body, light weight (300) for the masthead and opened-piece title;
- fonts bundled with the app (both are OFL), not loaded from Google at
  runtime.

**Front page** (approved mockup "broadsheet, by shape"):

- Masthead, centred: mark, "One sip at a time", a rule; a mono row with the
  long date ("Friday, 16 October 2026") left and "30 pieces · ~45 min" right.
- Two columns, roughly 1.6 : 1, separated by a hairline:
  - left: kicker "Lead", large headline, dek, reading time; then kicker
    "Longer reads" with headline, dek, reading time for each;
  - right: kicker "Briefs", each brief as its text (first ~280 chars),
    separated by hairlines. No headlines, no source labels.
- Single column below ~48rem width.

**Opened piece**:

- "← One sip at a time" back link; headline (light, large); dek; a mono
  byline row between hairlines: `By <Author>`, `<Source> · <channel>`,
  `<weekday day month, HH:MM>`, `<n> min`; then the body in the serif at a
  comfortable measure (~65ch); at the end "open original ↗" (when there is
  one) and "next piece →" following front-page order.

### HTML (first surface, my call)

`sip` builds the edition and serves it on `localhost` (random free port),
opening the default browser. Go `html/template`, CSS and fonts embedded with
`embed`. Pages: `/` front page, `/p/{n}` piece, `/edition.pdf`. Rationale:
it is the foundation the native window and the PDF both reuse.

### PDF

The same templates with a print stylesheet (A4, front page on page one, each
piece starting on a new page, page footer with the date), converted with
WeasyPrint (`weasyprint` on `PATH`, already installed; the user's
`make-pdf` uses it). `sip pdf [-o file]` writes it; the HTML surface links
to it.

### Terminal (second surface)

A Bubble Tea front page in the same structure (masthead, lead, longer reads,
briefs as one navigable list) and a reader reusing `render.Message` and the
existing reader view's link navigation. Colours approximated from the
palette with lipgloss; the mark omitted or as a tiny text glyph.

### Native window (later)

A webview shell around the HTML surface (Wails or a minimal macOS
WKWebView). Not designed further now.

## Repository layout

One Go module; the module path stays `nmail` to avoid churn (my call).

```
cmd/nmail/main.go          nmail entrypoint (moved from ./main.go)
cmd/sip/main.go            sip entrypoint
internal/mailbox/          shared IMAP (adds a window query)
internal/account/          shared keychain (service name becomes a parameter)
internal/render/           shared: email → terminal text
internal/ui/               nmail's TUI (unchanged)
internal/sip/piece/        Piece, email → Piece, fediverse item → Piece
internal/sip/edition/      Compose, Ranker
internal/sip/web/          HTML templates, assets, local server, PDF
internal/sip/tui/          terminal surface
internal/sip/warmhole/     reader API client (next milestone)
```

Shared-code changes:

- `mailbox.Session` gains `Window(from, to time.Time) ([]*Message, error)`
  returning full messages in INBOX in that window, read-only. `Message` gains
  the headers sip needs (`MessageID`, `ListID`, `ListPost`).
- `account.Keyring` gains a `Service` field; nmail keeps `"nmail"`, sip uses
  `"sip"`. Existing saved nmail logins keep working.
- nmail's behaviour and tests stay as they are; `go build ./cmd/nmail` must
  produce the same app.

## Commands

- `sip` — build today's edition and open it in the browser (HTML surface).
- `sip pdf [-o path]` — write the edition as PDF (default
  `~/Documents/One sip at a time/YYYY-MM-DD.pdf`, my call).
- `sip tui` — terminal surface (second milestone).
- `sip login` — prompt for the IMAP password (no echo) and save it to the
  keychain under service `sip`.

## Testing

- `edition.Cutoff`: before/after 06:30, DST transition days.
- `edition.Compose`: window boundaries, lead choice, the 3-minute split, empty
  edition.
- Email → Piece: fixtures (`.eml`) for a plain-text newsletter, an HTML-only
  newsletter with a preheader and tracking pixel, one with `List-Id`, and
  nmail's existing `testdata/digest.html`.
- Cleaning: tracking pixels and scripts removed, links and images kept.
- Templates: golden HTML for a fixture edition.
- `mailbox.Window`: against the existing IMAP test setup in
  `internal/mailbox/imap_test.go`.
- warmhole client (next milestone): against a fake server that serves the
  OpenAPI examples.

## Milestones

1. **Monorepo**: move nmail to `cmd/nmail`, parameterize the keychain
   service; nmail unchanged.
2. **Email edition in the browser**: window query, Piece, Compose, HTML
   front page and piece pages, `sip login`.
3. **PDF**.
4. **Terminal surface**.
5. **Fediverse** via the warmhole contract.
6. **Native window**.
7. **Scheduled build**: a launchd job at 06:30 that writes the day's PDF, so
   the paper is waiting (optional; the stateless cutoff already makes the
   edition fixed).

## Left to the builder

Template structure and CSS details within the visual design above; the exact
first-sentence and preheader heuristics; the HTML sanitizer library;
Bubble Tea component structure; test fixture content.
