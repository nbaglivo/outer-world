# sip: fediverse pieces from stw

Milestone 5 of [One sip at a time](2026-10-16-one-sip-at-a-time-design.md):
the morning edition also carries what the accounts the user follows posted
and boosted, pulled from their stw (spread-the-word, formerly warmhole)
server's reader API.

## Intent

The paper should be made from everything the user subscribed to, not only
newsletters. Fediverse posts become pieces like any other and are laid out by
shape, never grouped by source.

## Goals

- Each edition includes the stw items that arrived in the edition window.
- A broken, missing or slow stw never stops the paper: the edition is built
  from email alone and the problem is logged.
- One-time setup: the reader token lives in the keychain.

## Non-goals

- Managing follows (`/following` endpoints).
- Looking up single items, tracking deletions, caching anything locally.
- Implementing the reader API in stw (it doesn't exist yet; this is built
  against the contract).

## Anti-goals

- An edition that fails or hangs because stw is down.
- A half-fetched fediverse section presented as complete.
- Remote HTML reaching the page uncleaned.

## Constraints

- Contract: stw's `docs/reader-api.openapi.json` (repo
  `nbaglivo/spread-the-word`), design in its
  `docs/superpowers/specs/2026-10-16-reader-api-design.md`.
- Stateless, like the rest of sip: every run fetches the window again.
- Go standard library HTTP client; no generated client.

## Approaches

- **Hand-written client for the one endpoint, fetched during the build**
  (chosen). Small, fits the stateless design, testable with a fake server.
- Generated client from the OpenAPI file: adds a generator and dependency for
  a single GET; rejected.
- Background sync into a local store: breaks the stateless design; rejected.

## Decisions

### Fetching (`internal/sip/stw`)

- `GET {base}/reader/v1/sites/{site}/items?since=<window start>&until=<cutoff>&order=asc&limit=200`,
  `Authorization: Bearer <token>`, `Accept: application/json`. Timestamps
  sent as RFC 3339 UTC.
- Follows `next_cursor` (passing `cursor` plus the same `since`, `until`,
  `order`) until the cursor is null or a page has no items.
- Cap of 25 pages and a 15 second timeout for the whole fetch (my call).
- The API window is `(since, until]`; `edition.Compose` still applies its own
  `[start, cutoff)` filter, so the paper's window rule is unchanged.
- Any error (network, timeout, non-2xx, bad JSON, page cap hit) fails the whole
  fetch: pages already read are dropped. Non-2xx errors carry the contract's
  `{error, code}` body when present, else the status.

### Configuration

- `-stw` / `SIP_STW_URL`, default `https://ap.nbaglivo.dev`.
- `-site` / `SIP_STW_SITE`, default `inner-monologue`.
- `sip login fediverse` prompts for the reader token without echo and saves it
  in the keychain, service `sip`, as its own item (separate from the IMAP
  login, which `sip login` keeps saving as today).

### Errors and logging

- No saved token: `sip: fediverse skipped: run sip login fediverse` on stderr,
  edition from email.
- Fetch failed: `sip: fediverse: <error>` on stderr, edition from email.
- Email fetch failures keep their current behaviour.

### Item → Piece (`piece.FromItem`)

As in the main design's table:

- `ID` ← `id`; `Title` ← `title` (empty for notes); `Dek` ← `summary`, else
  the first sentence of `content_text`, clipped like email.
- `Author` ← `author.name`, else `author.handle`; `Source` ← host of
  `author.id`; `Channel` = `fediverse`.
- `Published` ← `published`; `Arrived` ← `received_at`.
- `HTML` ← `content_html` passed through the same `piece.Clean` as email (my
  call: stw isn't built yet and the HTML is written by other people), then
  attachments appended: images as `<img>` with `description` as alt, other
  kinds as links.
- `Text` ← `content_text`; `Words` from it; `Original` ← `url` (http/https
  only); `Raw` empty.
- New field `BoostedBy` (the booster's name, else handle); the piece byline
  shows "boosted by …" when set.
- Replies, sensitive posts and unknown types are shown like any other item;
  for sensitive posts the content warning is the dek (my call).

### Wiring

`buildEdition` takes the email pieces and the fediverse pieces and composes
them together; the fediverse source is an interface so tests can fake it.

## Testing

- Fake stw (`httptest`) serving the contract's examples: two-page paging,
  the caught-up empty page, 401 with an error body, invalid JSON, a timeout,
  and the page cap.
- Request shape: path, query parameters, bearer header.
- `FromItem`: every field for the Note and boosted Article examples,
  attachments, sensitive dek, non-http `url` dropped.
- `buildEdition`: fediverse error and missing token still produce the email
  edition and the expected stderr line.
- A page test for the "boosted by" byline (goldens stay unchanged).

## Left to the builder

Package internals, the keychain item's account name, test fixture layout,
exact byline markup for boosts within the existing style.
