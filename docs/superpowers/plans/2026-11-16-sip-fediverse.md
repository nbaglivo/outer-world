# sip fediverse pieces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the posts and boosts from the user's mastodon.social home timeline to sip's morning edition, so fediverse posts become pieces next to newsletters, and a failing Mastodon only produces a warning.

**Architecture:** A small `internal/sip/mastodon` client reads `GET /api/v1/timelines/home` newest first, paging with `max_id` until a page is entirely older than the window start (15 s budget, 25-page cap), and returns the statuses plus whether the timeline reached the window start. `piece.FromStatus` turns each status into a `piece.Piece` (HTML cleaned by the existing `piece.Clean`). `cmd/sip.buildEdition` takes the email window and a list of named feeds (only `mastodon` for now; Bluesky is the next milestone); any feed problem goes to stderr and the edition is built without it. The access token lives in the keychain as a single secret.

**Tech Stack:** Go (via mise), `net/http`, `encoding/json`, `net/http/httptest`, `github.com/zalando/go-keyring` (already a dependency).

**Spec:** `docs/superpowers/specs/2026-11-16-sip-fediverse-design.md` (parent design: `docs/superpowers/specs/2026-10-16-one-sip-at-a-time-design.md`). API: https://docs.joinmastodon.org/methods/timelines/ and the `Status`, `Account`, `MediaAttachment`, `PreviewCard` entities.

## Global Constraints

- No new module dependencies.
- Flag/env: `-mastodon` / `SIP_MASTODON_URL`, default `https://mastodon.social`.
- Request: `GET {base}/api/v1/timelines/home?limit=40[&max_id=<id>]`, `Authorization: Bearer <token>`, `Accept: application/json`.
- Whole fetch: 15 second timeout, at most 25 pages; any error drops all pages already read.
- stderr lines, exact: `sip: mastodon skipped: run sip login mastodon`, `sip: mastodon: <error>`, `sip: mastodon: timeline only reaches back to <RFC 3339 time in the edition's zone>`.
- Keychain: service `sip`; the token is its own item (account name `mastodon-token`), the IMAP login item is untouched.
- `Channel` for Mastodon pieces is `fediverse`.
- Conventional commits, lowercase; config files comment-free.

## Review Focus

- A proxy answers 200 with an HTML page → a `sip: mastodon:` warning, never a crash or a silent empty section. (Task 2, `TestHomeNotJSON`.)
- Mastodon answers 206 "home feed is regenerating" → treated as an error, not as a short timeline. (Task 2, `TestHomePartialContent`.)
- `-mastodon https://mastodon.social/` (trailing slash) → requests `/api/v1/timelines/home`, no `//`. (Task 2, `TestHomeTrailingSlash`.)
- A boost of a week-old post, boosted inside the window → kept, `Arrived` = boost time, `Published` = post time. (Tasks 2 and 3.)
- An attachment or card with a `javascript:` URL, a null attachment `url`, or alt text with `"`/`<` → link dropped, text escaped. (Task 3, `TestFromStatusIsSafe`.)
- A pasted token with a trailing newline or spaces → saved trimmed. (Task 5, `TestLoginTokenTrims`.)

---

### Task 1: Keychain secret for the access token

**Files:**
- Create: `internal/account/secret.go`
- Test: `internal/account/secret_test.go`

**Interfaces:**
- Produces: `type Secret struct{ Service, Name string }`; `func (s Secret) Get() (string, error)` returns `"", nil` when absent; `func (s Secret) Set(v string) error`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSecretRoundTrip(t *testing.T) {
	keyring.MockInit()
	s := Secret{Service: "sip", Name: "mastodon-token"}
	if v, err := s.Get(); err != nil || v != "" {
		t.Fatalf("empty Get = %q, %v", v, err)
	}
	if err := s.Set("tok"); err != nil {
		t.Fatal(err)
	}
	if v, _ := s.Get(); v != "tok" {
		t.Fatalf("Get = %q, want tok", v)
	}
}

func TestSecretLeavesLoginsAlone(t *testing.T) {
	keyring.MockInit()
	k := Keyring{Service: "sip"}
	k.Save([]mailbox.Credentials{{Host: "h", Username: "u", Password: "pw"}})
	Secret{Service: "sip", Name: "mastodon-token"}.Set("tok")
	if list, err := k.Load(); err != nil || len(list) != 1 || list[0].Password != "pw" {
		t.Fatalf("logins = %+v, %v", list, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/account/ -run Secret`: expect FAIL (undefined: Secret).
- [ ] **Step 3: Implement `Secret`** with `keyring.Get/Set(s.Service, s.Name, …)`; map `keyring.ErrNotFound` to `"", nil`; wrap other errors as `load <Name>: %w` / `save <Name>: %w`.
- [ ] **Step 4: Run** `go test ./internal/account/`: expect PASS.
- [ ] **Step 5: Commit** `feat: keep a single secret in the keychain`

---

### Task 2: Mastodon home timeline client

**Files:**
- Create: `internal/sip/mastodon/mastodon.go`
- Create: `internal/sip/mastodon/testdata/home.json` (a page of statuses, see Step 1)
- Test: `internal/sip/mastodon/mastodon_test.go`

**Interfaces:**
- Produces:

```go
type Account struct {
	Acct        string `json:"acct"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
}
type Attachment struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	Description string `json:"description"`
}
type Card struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}
type Status struct {
	ID          string       `json:"id"`
	URI         string       `json:"uri"`
	URL         string       `json:"url"`
	CreatedAt   time.Time    `json:"created_at"`
	Account     Account      `json:"account"`
	Content     string       `json:"content"`
	SpoilerText string       `json:"spoiler_text"`
	Sensitive   bool         `json:"sensitive"`
	Reblog      *Status      `json:"reblog"`
	Attachments []Attachment `json:"media_attachments"`
	Card        *Card        `json:"card"`
}
type Timeline struct {
	Statuses []Status // newest first, all pages read
	Complete bool     // false: the timeline ran out before reaching since
	Oldest   time.Time
}
type Client struct {
	BaseURL, Token string
	HTTP           *http.Client  // nil: http.DefaultClient
	Timeout        time.Duration // 0: 15 * time.Second
}
const MaxPages = 25
func (c Client) Home(ctx context.Context, since time.Time) (Timeline, error)
```

(JSON `null` leaves a field at its zero value; unknown fields are ignored.)

- [ ] **Step 1: Fixture.** `testdata/home.json`, three statuses newest first, shaped like the docs' `Status` examples:
  - `"300"`: a boost by `carol@example.social` (display name `Carol`) at 2026-10-15T11:00:00Z of `reblog` `"299"`, a post by `bob@write.as` (`Bob's notebook`, `url` `https://write.as/bob`) created 2026-10-08T09:00:00Z, content `<p>Autumn reading: five books for long evenings.</p>`, `card` `{title: "Autumn reading", url: "https://write.as/bob/autumn-reading"}`.
  - `"200"`: a post by `alice` (`Alice`, `url` `https://mastodon.social/@alice`) at 2026-10-15T09:00:00Z, content `<p>First pour-over of the season. The kettle finally arrived.</p>`, `url` `https://mastodon.social/@alice/200`, one `image` attachment `https://files.mastodon.social/kettle.jpg` with description `A gooseneck kettle pouring water over a paper filter.`.
  - `"100"`: a post by `dave@fosstodon.org` at 2026-10-14T03:00:00Z with `spoiler_text` `Spoilers for the last chapter`, `sensitive: true`.

- [ ] **Step 2: Write the failing tests** (an `httptest.Server` per test; helper `serve(t, h http.HandlerFunc) Client` with `Token: "tok"`; `since` = 2026-10-15T04:30:00Z unless stated).

- `TestHomeRequest`: server records requests, serves `home.json` then `[]`. Assert first path `/api/v1/timelines/home`, query exactly `limit=40`; headers `Authorization: Bearer tok`, `Accept: application/json`.
- `TestHomePagesUntilOlder`: page 1 = statuses `300`, `200`; page 2 (asserting `max_id=200`, `limit=40`) = status `100`; no third request. Result: 3 statuses, `Complete == true`, `Statuses[0].Reblog.Account.DisplayName == "Bob's notebook"`, `Statuses[1].Attachments[0].Type == "image"`.
- `TestHomeKeepsPagesWithSomeNewer`: page 1 = `200`, `100` (mixed) → a second request is made (only a page wholly older than `since` stops).
- `TestHomeShortTimeline`: page 1 = `300`, `200`; page 2 = `[]` → no error, `Complete == false`, `Oldest == 2026-10-15T09:00:00Z`, 2 statuses.
- `TestHomeEmpty`: page 1 = `[]` → no error, `Complete == false`, `Oldest` zero, no statuses.
- `TestHomeUnauthorized`: 401 with `{"error":"The access token is invalid"}` → error containing `401` and `The access token is invalid`, empty timeline.
- `TestHomePartialContent`: 206 with a JSON array → error containing `regenerating`.
- `TestHomeNotFound`: 404, empty body → error containing `404`.
- `TestHomeNotJSON`: 200, `Content-Type: text/html`, body `<html>hi</html>` → error.
- `TestHomeTimeout`: handler sleeps 200 ms; `Timeout: 50 * time.Millisecond` → error.
- `TestHomePageCap`: handler always returns one status newer than `since` with a fresh id → error mentioning `25 pages`; exactly `MaxPages` requests.
- `TestHomeDropsPartialPages`: page 1 = `300`, `200`; page 2 = 500 → error, empty timeline.
- `TestHomeTrailingSlash`: `BaseURL: srv.URL + "/"` → path is exactly `/api/v1/timelines/home`.

- [ ] **Step 3: Run** `go test ./internal/sip/mastodon/`: expect FAIL (package has no code).
- [ ] **Step 4: Implement `Client.Home`.** Wrap ctx with the timeout; base is `strings.TrimRight(BaseURL, "/")`. Loop: request with `limit=40` and, after the first page, `max_id` = last status's `ID`; empty page → return `Complete: false` with `Oldest` = last status's `CreatedAt`; page where every status's `CreatedAt` is before `since` → append it and return `Complete: true`; else append and continue; after `MaxPages` requests return `more than 25 pages`. Status 206 → `GET home: 206: home feed is regenerating`. Other non-2xx: decode `{error}` if possible and return `GET home: <status>: <error>`, else `GET home: <status>`. Any error returns `Timeline{}`.
- [ ] **Step 5: Run** `go test ./internal/sip/mastodon/`: expect PASS.
- [ ] **Step 6: Commit** `feat: read the mastodon home timeline`

---

### Task 3: Status → Piece

**Files:**
- Create: `internal/sip/piece/status.go`
- Modify: `internal/sip/piece/piece.go` (add `BoostedBy string` to `Piece`)
- Test: `internal/sip/piece/status_test.go` (reuses `../mastodon/testdata/home.json`)

**Interfaces:**
- Consumes: `mastodon.Status`, `mastodon.Account`, `mastodon.Attachment`, `mastodon.Card` (Task 2); `Clean`, `firstSentence`, `clip` (existing).
- Produces: `func FromStatus(s mastodon.Status) Piece`; field `Piece.BoostedBy string`.

- [ ] **Step 1: Write the failing tests.** Load the statuses from `home.json`.

- `TestFromStatusPost` (`200`, alice): `ID` = its `uri`; `Title == ""`; `Dek == "First pour-over of the season."`; `Author == "Alice"`; `Source == "mastodon.social"`; `Channel == "fediverse"`; `Published == Arrived == 2026-10-15T09:00:00Z`; `Text` contains `The kettle finally arrived.`; `Words == len(strings.Fields(Text))`; `Original == "https://mastodon.social/@alice/200"`; `BoostedBy == ""`; `HTML` contains `<img src="https://files.mastodon.social/kettle.jpg" alt="A gooseneck kettle pouring water over a paper filter."`; `Raw == nil`; `Headline() == Dek`.
- `TestFromStatusBoost` (`300`): `ID` = the reblog's `uri`; `Author == "Bob's notebook"`; `Source == "write.as"`; `BoostedBy == "Carol"`; `Published == 2026-10-08T09:00:00Z`; `Arrived == 2026-10-15T11:00:00Z`; `HTML` contains `<a href="https://write.as/bob/autumn-reading"` and `Autumn reading`.
- `TestFromStatusContentWarning` (`100`): `Dek == "Spoilers for the last chapter"`.
- `TestFromStatusFallbacks`: status with empty `DisplayName`, `Acct: "z@x.example"`, empty `Account.URL`, empty `URL`, `URI: "https://x.example/users/z/statuses/1"`, plus a boost of it by an account with only `Acct: "b@y.example"` → `Author == "z@x.example"`, `Source == "x.example"`, `Original == URI`, `BoostedBy == "b@y.example"`.
- `TestFromStatusIsSafe`: attachments `{Type: "image", URL: "javascript:alert(1)"}`, `{Type: "image", URL: ""}`, `{Type: "audio", URL: "https://x.example/a.mp3", Description: "a \"song\" <b>"}`; `Card{Title: "t", URL: "javascript:x"}`; `URL: "javascript:x"`, `URI: "tag:x"` → `HTML` has no `javascript:`; contains `<a href="https://x.example/a.mp3"` and `&lt;b&gt;` (no raw `<b>`); `Original == ""`.
- `TestFromStatusStripsScript`: `Content: "<p>hi</p><script>x()</script>"` → `HTML` has no `script`.

- [ ] **Step 2: Run** `go test ./internal/sip/piece/ -run FromStatus`: expect FAIL.
- [ ] **Step 3: Implement `FromStatus`.** `post` = `*s.Reblog` if set, else `s`. Mapping per the spec's "Status → Piece" section. Markup = `post.Content` + per attachment with an http/https URL (values `html.EscapeString`d): `image` as `<p><img src="…" alt="…"></p>`, others as `<p><a href="…">description, else "attachment"</a></p>`; + the card, if its URL is http/https, as `<p><a href="…">title, else URL</a></p>`. Pass it through `Clean`; `HTML` and `Text` from the result. `Dek` = `clip(SpoilerText)`, else `firstSentence(Text)`. `Source` = host of `Account.URL`, else the part after `@` in `Acct`. `Original` = `URL`, else `URI`, only if http/https.
- [ ] **Step 4: Run** `go test ./internal/sip/piece/`: expect PASS (email tests unchanged).
- [ ] **Step 5: Commit** `feat: turn mastodon statuses into pieces`

---

### Task 4: "boosted by" in the byline

**Files:**
- Modify: `internal/sip/web/templates/piece.html` (byline row)
- Test: `internal/sip/web/web_test.go`

**Interfaces:**
- Consumes: `Piece.BoostedBy` (Task 3).

- [ ] **Step 1: Write the failing test** `TestPieceShowsBoost`: an edition whose lead has `Author: "Bob's notebook"`, `BoostedBy: "Carol"`, `Channel: "fediverse"`; GET `/p/1`; body contains `<span>boosted by Carol</span>` right after the `By Bob&#39;s notebook` span. Also assert the existing piece fixture's page does not contain `boosted by`.
- [ ] **Step 2: Run** `go test ./internal/sip/web/ -run Boost`: expect FAIL.
- [ ] **Step 3: Add** `{{with .BoostedBy}}<span>boosted by {{.}}</span>{{end}}` after the author span.
- [ ] **Step 4: Run** `go test ./internal/sip/web/`: expect PASS with goldens unchanged (no fixture has a boost).
- [ ] **Step 5: Commit** `feat: show who boosted a piece`

---

### Task 5: Wire feeds into sip

**Files:**
- Modify: `cmd/sip/edition.go` (`feed`, `buildEdition`, `mastodonFeed`)
- Modify: `cmd/sip/main.go` (flag, `login mastodon`, building the feeds)
- Modify: `cmd/sip/login.go` (`loginToken`, `promptToken`)
- Test: `cmd/sip/main_test.go`

**Interfaces:**
- Consumes: `mastodon.Client.Home`, `mastodon.Timeline` (Task 2); `piece.FromStatus` (Task 3); `account.Secret` (Task 1).
- Produces:

```go
// feed is a source of pieces besides email. A nil fetch means no saved token.
type feed struct {
	name  string
	fetch func(since, until time.Time, note func(string)) ([]piece.Piece, error)
}
func buildEdition(w windower, feeds []feed, now time.Time, loc *time.Location, warn io.Writer) (edition.Edition, error)
func mastodonFeed(home func(ctx context.Context, since time.Time) (mastodon.Timeline, error), loc *time.Location) feed
func loginToken(store interface{ Set(string) error }, readToken func() (string, error)) error
```

- [ ] **Step 1: Write the failing tests** (update the existing `buildEdition` calls to pass `nil` feeds).

- `TestBuildEditionAddsFeed`: `fakeWindow` with one email arriving 2026-10-15 09:00 UTC; a feed `"mastodon"` whose fetch records `since`/`until` and returns one piece with `Arrived` 2026-10-15 10:00 UTC, `Channel: "fediverse"`. With `now` = 2026-10-16 08:00 UTC: `since == edition.Start(cutoff)`, `until == cutoff`, `ed.Count() == 2`, `warn` empty.
- `TestBuildEditionFeedError`: fetch returns `errors.New("GET home: 404")` → no error, `ed.Count() == 1`, `warn == "sip: mastodon: GET home: 404\n"`.
- `TestBuildEditionFeedSkipped`: `feed{name: "mastodon"}` (nil fetch) → `ed.Count() == 1`, `warn == "sip: mastodon skipped: run sip login mastodon\n"`.
- `TestBuildEditionFeedNote`: fetch calls `note("timeline only reaches back to X")` and returns one piece → `ed.Count() == 2`, `warn == "sip: mastodon: timeline only reaches back to X\n"`.
- `TestBuildEditionEmailErrorStillFails`: `fakeWindow{err: boom}` → error wraps `boom` and no fetch is called.
- `TestMastodonFeed`: `home` returns a `Timeline` with the `home.json` statuses and `Complete: true` → 3 pieces, no note; with `Complete: false, Oldest: 2026-10-15T09:00:00Z` and `loc` Europe/Madrid → note `timeline only reaches back to 2026-10-15T11:00:00+02:00`; `home` error → same error back.
- `TestLoginToken`: `keyring.MockInit()`; `loginToken(account.Secret{Service: "sip", Name: "mastodon-token"}, func() (string, error) { return "tok", nil })` → `Get() == "tok"`; existing IMAP logins under `sip` still load.
- `TestLoginTokenTrims`: input `"  tok\n"` → saved `"tok"`.
- `TestLoginTokenEmpty`: input `"  "` → error containing `token`.

- [ ] **Step 2: Run** `go test ./cmd/sip/`: expect FAIL.
- [ ] **Step 3: Implement.**
  - `buildEdition`: fetch email first (errors stay fatal, unchanged); then for each feed: nil fetch → `sip: <name> skipped: run sip login <name>`; else call it with a `note` that prints `sip: <name>: <text>`; on error print `sip: <name>: <err>` and add nothing; compose all pieces together.
  - `mastodonFeed`: calls `home(context.Background(), since)`, maps statuses with `piece.FromStatus`, and when `!Complete && !Oldest.IsZero()` notes `timeline only reaches back to <Oldest.In(loc).Format(time.RFC3339)>`. `until` is unused (Compose filters).
  - `main.go`: add `-mastodon` (`SIP_MASTODON_URL`, default `https://mastodon.social`); accept `login mastodon`, which needs neither `-host` nor `-user`, next to `login`; usage becomes `usage: sip [-host h] [-port p] [-user u] [-mastodon url] [login [mastodon]]`. In `run`, read `account.Secret{Service: "sip", Name: "mastodon-token"}`: empty → `feed{name: "mastodon"}`; keychain error → a feed whose fetch returns that error; else `mastodonFeed(mastodon.Client{BaseURL, Token}.Home, loc)`.
  - `login.go`: `loginToken` trims spaces, rejects empty with `empty token`, calls `Set`; `promptToken(base string)` first prints `Make a token at <base>/settings/applications with only the read:statuses scope.` then prompts `Access token for <base>: ` with `term.ReadPassword`.
- [ ] **Step 4: Run** `go test ./... && go vet ./...`: expect PASS.
- [ ] **Step 5: Smoke run.** With no token saved, `go run ./cmd/sip -host … -user …` prints the skipped line and serves the email edition. After `go run ./cmd/sip login mastodon` with a real `read:statuses` token, it serves an edition with fediverse pieces; with `-mastodon http://127.0.0.1:9` it prints one `sip: mastodon:` line and still serves the edition.
- [ ] **Step 6: Commit** `feat: add the mastodon timeline to the edition`

---

### Task 6: Point the parent design at Mastodon

**Files:**
- Modify: `docs/superpowers/specs/2026-10-16-one-sip-at-a-time-design.md`

- [ ] **Step 1:** Replace the "Fediverse" section, the fediverse column of the piece table, the `internal/sip/warmhole/` line, the client testing line and milestone 5 with Mastodon equivalents that link to the milestone spec; add "Bluesky" as the next milestone.
- [ ] **Step 2: Commit** `docs: read the fediverse from mastodon`
