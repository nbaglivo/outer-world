# sip: fediverse pieces from Mastodon

Milestone 5 of [One sip at a time](2026-10-16-one-sip-at-a-time-design.md):
the morning edition also carries what the accounts the user follows on
mastodon.social posted and boosted, read from the Mastodon client API.

This replaces the earlier plan to read stw's reader API: stw
(spread-the-word) stays publish-only and the reader API won't be built.

## Intent

The paper should be made from everything the user subscribed to, not only
newsletters. Fediverse posts become pieces like any other and are laid out by
shape, never grouped by source.

## Goals

- Each edition includes the posts and boosts from the user's mastodon.social
  home timeline that fall in the edition window.
- A broken, slow or unreachable Mastodon never stops the paper: the edition is
  built from email alone and the problem is logged.
- One-time setup: the access token lives in the keychain.
- Feeds are pluggable: Bluesky (next milestone) adds a feed without changing
  how the edition is built.

## Non-goals

- Bluesky (next milestone, its own client against `app.bsky.feed.getTimeline`).
- Managing follows, notifications, lists, or anything that writes to Mastodon.
- Tracking edits and deletions, caching anything locally.
- Hiding posts the user's Mastodon filters mark (shown like any other post).

## Anti-goals

- An edition that fails or hangs because Mastodon is down.
- A half-fetched fediverse section presented as complete.
- Remote HTML reaching the page uncleaned.

## Constraints

- API: `GET /api/v1/timelines/home`, documented at
  https://docs.joinmastodon.org/methods/timelines/ and the `Status`,
  `Account`, `MediaAttachment` and `PreviewCard` entities.
- The token has only the `read:statuses` scope.
- Stateless, like the rest of sip: every run fetches the window again.
- Go standard library HTTP client; no Mastodon library.

## Approaches

- **Hand-written client for the home timeline, fetched during the build**
  (chosen). One GET, fits the stateless design, testable with a fake server.
- A Mastodon client library: a dependency for a single GET; rejected.
- Bridgy Fed for Bluesky (follow bridged accounts from mastodon.social):
  only covers Bluesky accounts that opted into the bridge; rejected as the
  Bluesky answer, though bridged accounts the user does follow show up like
  any other post.

## Decisions

### Fetching (`internal/sip/mastodon`)

- `GET {base}/api/v1/timelines/home?limit=40`, `Authorization: Bearer
  <token>`, `Accept: application/json`; following pages add
  `max_id=<id of the last status on the previous page>`. The timeline comes
  newest first and can't be asked for a time range.
- Stops at the first page whose statuses are all older than the window start
  (by the outer status's `created_at`, so a late boost of an old post still
  counts), or at an empty page. Statuses newer than the cutoff are read and
  left to `edition.Compose`'s `[start, cutoff)` filter.
- If the timeline runs out (empty page) before reaching the window start, the
  posts read are kept and a line says how far back it went (my call: Mastodon
  keeps a bounded home feed, so a busy day can't be read further back).
- Cap of 25 pages (1000 statuses) and a 15 second timeout for the whole fetch
  (my call).
- Any error (network, timeout, non-2xx, 206 "home feed is regenerating", bad
  JSON, page cap hit) fails the whole fetch: pages already read are dropped.
  Non-2xx errors carry Mastodon's `{error}` body when present, else the
  status.

### Configuration

- `-mastodon` / `SIP_MASTODON_URL`, default `https://mastodon.social`.
- `sip login mastodon` prompts for the access token without echo and saves it
  in the keychain, service `sip`, as its own item (separate from the IMAP
  login, which `sip login` keeps saving as today). The prompt says where to
  make one: Preferences → Development → New application, scope
  `read:statuses`.

### Feeds

- `buildEdition` takes the email window and a list of feeds; each feed has a
  name (`mastodon`) and a fetch that returns pieces for `[since, until)`.
  Bluesky will be a second entry.
- A feed with no saved token is skipped with a line; a feed that fails adds
  nothing. Neither affects other feeds or email.

### Errors and logging

- No saved token: `sip: mastodon skipped: run sip login mastodon` on stderr.
- Fetch failed: `sip: mastodon: <error>` on stderr.
- Timeline too short: `sip: mastodon: timeline only reaches back to <time>`.
- Email fetch failures keep their current behaviour.

### Status → Piece (`piece.FromStatus`)

For a boost, the outer status says who boosted and when; the post shown is
its `reblog`.

- `ID` ← the post's `uri`; `Title` empty (Mastodon has none, my call; the
  headline falls back to the dek).
- `Dek` ← `spoiler_text` (the content warning), else the first sentence of
  the text, clipped like email.
- `Author` ← `account.display_name`, else `account.acct`; `Source` ← host of
  `account.url`, else the domain in `acct`; `Channel` = `fediverse`.
- `Published` ← the post's `created_at`; `Arrived` ← the outer status's
  `created_at` (when it reached the timeline).
- `HTML` ← `content`, then attachments (images as `<img>` with `description`
  as alt; `gifv`, video, audio and unknown as links; attachments with no URL
  dropped), then the link card as a link titled `card.title`, all through
  `piece.Clean` like email.
- `Text` ← the cleaned text; `Words` from it; `Original` ← the post's `url`,
  else `uri` (http/https only); `Raw` empty.
- New field `BoostedBy` (the booster's display name, else `acct`); the piece
  byline shows "boosted by …" when set.
- Replies, sensitive posts and posts from followed hashtags are shown like any
  other post.

## Testing

- Fake Mastodon (`httptest`) serving status JSON: request shape (path,
  `limit`, `max_id`, bearer header), paging until a page is older than the
  window, stopping on an empty page, the short-timeline note, 401 with
  `{"error": …}`, 206, invalid JSON, a timeout, the page cap, trailing slash
  on the base URL.
- `FromStatus`: a plain post with an image, a boost, a content warning,
  attachments with unsafe or missing URLs, a link card, null `url`.
- `buildEdition`: feed error, missing token and short timeline still produce
  the email edition plus the expected stderr lines.
- A page test for the "boosted by" byline (goldens stay unchanged).

## Left to the builder

Package internals, the keychain item's account name, test fixture layout,
exact byline markup for boosts within the existing style.
