# sip fediverse pieces Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Add the items from stw's reader API to sip's morning edition, so fediverse posts become pieces next to newsletters, and a failing stw only produces a warning.

**Architecture:** A small `internal/sip/stw` client fetches the edition window from `GET /reader/v1/sites/{site}/items` (asc, cursor paging, 15 s budget, 25-page cap) and returns `[]stw.Item`. `piece.FromItem` turns each item into a `piece.Piece` (HTML cleaned by the existing `piece.Clean`). `cmd/sip.buildEdition` composes email and fediverse pieces together; any fediverse problem goes to stderr and the edition is built from email alone. The reader token lives in the keychain as a single secret.

**Tech Stack:** Go (via mise), `net/http`, `encoding/json`, `net/http/httptest`, `github.com/zalando/go-keyring` (already a dependency).

**Spec:** `docs/superpowers/specs/2026-11-16-sip-fediverse-design.md` (parent design: `docs/superpowers/specs/2026-10-16-one-sip-at-a-time-design.md`). Contract: `~/devel/warmhole/docs/reader-api.openapi.json` (repo being renamed to spread-the-word, "stw").

## Global Constraints

- Name the server **stw** everywhere in code, flags, env vars and messages; never "warmhole".
- No new module dependencies.
- Flags/env: `-stw` / `SIP_STW_URL` default `https://ap.nbaglivo.dev`; `-site` / `SIP_STW_SITE` default `inner-monologue`.
- Request: `?since=<RFC 3339 UTC ms>&until=<RFC 3339 UTC ms>&order=asc&limit=200`, `Authorization: Bearer <token>`, `Accept: application/json`.
- Whole fetch: 15 second timeout, at most 25 pages; any error drops all pages already read.
- stderr lines, exact: `sip: fediverse skipped: run sip login fediverse` and `sip: fediverse: <error>`.
- Keychain: service `sip`; the token is its own item (account name `stw-reader`), the IMAP login item is untouched.
- `Channel` for fediverse pieces is `fediverse`.
- Conventional commits, lowercase; config files comment-free.

## Review Focus

- stw (or a proxy in front of it) answers 200 with an HTML page → a `sip: fediverse:` warning, never a crash or empty-but-silent section. (Task 2, `TestItemsNotJSON`.)
- `-stw` given with a trailing slash (`https://ap.nbaglivo.dev/`) → still requests `/reader/v1/...`, no `//`. (Task 2, `TestItemsTrailingSlash`.)
- An item with `null` `published`, `title` and `author.name` → piece with `Published` = `received_at`, `Author` = handle, no panic. (Task 3, `TestFromItemNulls`.)
- An attachment whose `url` is `javascript:` or whose description contains `"` / `<` → link dropped, description escaped. (Task 3, `TestFromItemAttachmentsAreSafe`.)
- A pasted token with a trailing newline or spaces → saved trimmed. (Task 5, `TestLoginTokenTrims`.)

---

### Task 1: Keychain secret for the reader token

**Files:**
- Create: `internal/account/secret.go`
- Test: `internal/account/secret_test.go`

**Interfaces:**
- Produces: `type Secret struct{ Service, Name string }`; `func (s Secret) Get() (string, error)` returns `"", nil` when absent; `func (s Secret) Set(v string) error`.

- [ ] **Step 1: Write the failing tests**

```go
func TestSecretRoundTrip(t *testing.T) {
	keyring.MockInit()
	s := Secret{Service: "sip", Name: "stw-reader"}
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
	Secret{Service: "sip", Name: "stw-reader"}.Set("tok")
	if list, err := k.Load(); err != nil || len(list) != 1 || list[0].Password != "pw" {
		t.Fatalf("logins = %+v, %v", list, err)
	}
}
```

- [ ] **Step 2: Run** `go test ./internal/account/ -run Secret` — expect FAIL (undefined: Secret).
- [ ] **Step 3: Implement `Secret`** with `keyring.Get/Set(s.Service, s.Name, …)`; map `keyring.ErrNotFound` to `"", nil`; wrap other errors as `load <Name>: %w` / `save <Name>: %w`.
- [ ] **Step 4: Run** `go test ./internal/account/` — expect PASS.
- [ ] **Step 5: Commit** `feat: keep a single secret in the keychain`

---

### Task 2: stw reader client

**Files:**
- Create: `internal/sip/stw/stw.go`
- Create: `internal/sip/stw/testdata/morning.json`, `internal/sip/stw/testdata/caughtup.json`
- Test: `internal/sip/stw/stw_test.go`

**Interfaces:**
- Produces:

```go
type Account struct {
	ID         string `json:"id"`
	Handle     string `json:"handle"`
	Name       string `json:"name"`
	ProfileURL string `json:"profile_url"`
}
type Boost struct {
	By Account `json:"by"`
}
type Attachment struct {
	URL         string `json:"url"`
	MediaType   string `json:"media_type"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}
type Item struct {
	ID          string       `json:"id"`
	URL         string       `json:"url"`
	Type        string       `json:"type"`
	Published   time.Time    `json:"published"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	Sensitive   bool         `json:"sensitive"`
	ContentHTML string       `json:"content_html"`
	ContentText string       `json:"content_text"`
	Author      Account      `json:"author"`
	Boost       *Boost       `json:"boost"`
	Attachments []Attachment `json:"attachments"`
	ReceivedAt  time.Time    `json:"received_at"`
}
type Client struct {
	BaseURL, Site, Token string
	HTTP                 *http.Client  // nil: http.DefaultClient
	Timeout              time.Duration // 0: 15 * time.Second
}
const MaxPages = 25
func (c Client) Items(ctx context.Context, since, until time.Time) ([]Item, error)
```

(JSON `null` leaves a field at its zero value; unknown fields are ignored, as the contract requires.)

- [ ] **Step 1: Fixtures.** Copy the contract's two `listItems` 200 examples verbatim (`.paths["/sites/{siteId}/items"].get.responses["200"].content["application/json"].examples.{morning,caughtUp}.value`) into `testdata/morning.json` and `testdata/caughtup.json`.

- [ ] **Step 2: Write the failing tests** (an `httptest.Server` per test; helper `serve(t, h http.HandlerFunc) Client` with `Token: "tok"`, `Site: "inner-monologue"`).

- `TestItemsRequest`: one call with `since = 2026-10-15T04:30:00Z`, `until = 2026-10-16T04:30:00Z` (pass them in a non-UTC zone, e.g. Europe/Madrid 06:30); server records the request and returns `caughtup.json` with `next_cursor` changed to `null`. Assert path `/reader/v1/sites/inner-monologue/items`, query `since=2026-10-15T04:30:00.000Z`, `until=2026-10-16T04:30:00.000Z`, `order=asc`, `limit=200`, no `cursor`; header `Authorization: Bearer tok`, `Accept: application/json`.
- `TestItemsPages`: first response `morning.json` (cursor `c1`), second (asserting `cursor=c1` plus the same `since`/`until`/`order`) returns `caughtup.json`; assert 2 items, `items[0].ID == "https://mastodon.social/users/alice/statuses/113284750912345678"`, `items[1].Boost.By.Name == "Carol"`, `items[0].Attachments[0].Kind == "image"`, `items[1].Title == "Autumn reading"`, and exactly 2 requests.
- `TestItemsStopsOnNullCursor`: one page with items and `next_cursor: null` → 1 request.
- `TestItemsUnauthorized`: 401 with `{"error":"Missing or wrong reader token","code":"unauthorized"}` → error containing `401`, `unauthorized` and `Missing or wrong reader token`, nil items.
- `TestItemsNotFound`: 404 with empty body → error containing `404`.
- `TestItemsNotJSON`: 200, `Content-Type: text/html`, body `<html>hi</html>` → error, nil items.
- `TestItemsTimeout`: handler sleeps 200 ms; `Timeout: 50 * time.Millisecond` → error, nil items.
- `TestItemsPageCap`: handler always returns one item and a fresh cursor → error mentioning `25 pages`; exactly `MaxPages` requests.
- `TestItemsDropsPartialPages`: first page `morning.json`, second 500 → nil items, error.
- `TestItemsTrailingSlash`: `BaseURL: srv.URL + "/"` → path is exactly `/reader/v1/sites/inner-monologue/items`.

- [ ] **Step 3: Run** `go test ./internal/sip/stw/` — expect FAIL (package has no code).
- [ ] **Step 4: Implement `Client.Items`.** Wrap ctx with the timeout; build the URL from `strings.TrimRight(BaseURL, "/")` and `url.PathEscape(Site)`; loop pages until `next_cursor` is null or the page has no items, failing after `MaxPages` with `more than 25 pages`. Non-2xx: decode `{error, code}` if possible and return `GET items: <status>: <error> (<code>)`, else `GET items: <status>`. Format times with `t.UTC().Format("2006-01-02T15:04:05.000Z")`.
- [ ] **Step 5: Run** `go test ./internal/sip/stw/` — expect PASS.
- [ ] **Step 6: Commit** `feat: read items from the stw reader api`

---

### Task 3: Item → Piece

**Files:**
- Create: `internal/sip/piece/item.go`
- Modify: `internal/sip/piece/piece.go` (add `BoostedBy string` to `Piece`)
- Test: `internal/sip/piece/item_test.go` (reuses `internal/sip/stw/testdata/morning.json` via `../stw/testdata/morning.json`)

**Interfaces:**
- Consumes: `stw.Item`, `stw.Account`, `stw.Attachment` (Task 2); `Clean`, `firstSentence`, `clip` (existing, `email.go`/`clean.go`).
- Produces: `func FromItem(it stw.Item) Piece`; field `Piece.BoostedBy string`.

- [ ] **Step 1: Write the failing tests.** Load the two items from `morning.json`.

- `TestFromItemNote` (alice): `ID` = the item id; `Title == ""`; `Dek == "First pour-over of the season."`; `Author == "Alice"`; `Source == "mastodon.social"`; `Channel == "fediverse"`; `Published`/`Arrived` = `published`/`received_at`; `Text == content_text`; `Words == 10`; `Original == "https://mastodon.social/@alice/113284750912345678"`; `BoostedBy == ""`; `HTML` contains `First pour-over` and `<img src="https://files.mastodon.social/media_attachments/files/113/284/750/original/kettle.jpg" alt="A gooseneck kettle pouring water over a paper filter."`; `Raw == nil`.
- `TestFromItemBoostedArticle` (bob): `Title == "Autumn reading"`, `Dek == "Five books for long evenings."`, `Author == "Bob's notebook"`, `Source == "write.as"`, `BoostedBy == "Carol"`.
- `TestFromItemSensitiveDek`: item with `Sensitive: true`, `Summary: "Spoilers for the last chapter"`, `ContentText: "The river was the narrator all along."` → `Dek == "Spoilers for the last chapter"`.
- `TestFromItemNulls`: item with zero `Published`, empty `Title`, `Author{ID: "https://x.example/users/z", Handle: "@z@x.example"}`, `ReceivedAt` set → `Published == ReceivedAt`, `Author == "@z@x.example"`, `Headline()` non-empty, booster falls back to handle when `By.Name` is empty.
- `TestFromItemAttachmentsAreSafe`: attachments `{URL: "javascript:alert(1)", Kind: "image"}`, `{URL: "https://x.example/a.mp3", Kind: "audio", Description: "a \"song\" <b>"}`, plus `URL: "javascript:x"` on the item → `HTML` has no `javascript:`; contains `<a href="https://x.example/a.mp3"` and the escaped description text (`&lt;b&gt;`, no raw `<b>`); `Original == ""`.
- `TestFromItemStripsScript`: `ContentHTML: "<p>hi</p><script>x()</script>"` → `HTML` has no `script`.

- [ ] **Step 2: Run** `go test ./internal/sip/piece/ -run FromItem` — expect FAIL.
- [ ] **Step 3: Implement `FromItem`.** Mapping per the spec's "Item → Piece" section. Build markup = `ContentHTML` + per attachment (http/https URLs only, attribute and text values `html.EscapeString`d): images as `<p><img src="…" alt="…"></p>`, others as `<p><a href="…">description, else "attachment"</a></p>`; pass the whole markup through `Clean` and use its `HTML`. `Text` = `ContentText`, else `Clean(...).Text`. `Dek` = `clip(Summary)`, else `firstSentence(Text)`. `Source` = host of `Author.ID`, else the part after the last `@` of `Author.Handle`. `Original` = `URL` only if it parses with scheme http/https.
- [ ] **Step 4: Run** `go test ./internal/sip/piece/` — expect PASS (email tests unchanged).
- [ ] **Step 5: Commit** `feat: turn stw items into pieces`

---

### Task 4: "boosted by" in the byline

**Files:**
- Modify: `internal/sip/web/templates/piece.html` (byline row)
- Test: `internal/sip/web/web_test.go`

**Interfaces:**
- Consumes: `Piece.BoostedBy` (Task 3).

- [ ] **Step 1: Write the failing test** `TestPieceShowsBoost`: an edition whose lead has `Author: "Bob's notebook"`, `BoostedBy: "Carol"`, `Channel: "fediverse"`; GET `/p/1`; body contains `<span>boosted by Carol</span>` right after the `By Bob&#39;s notebook` span. Also assert the existing piece fixture's page does not contain `boosted by`.
- [ ] **Step 2: Run** `go test ./internal/sip/web/ -run Boost` — expect FAIL.
- [ ] **Step 3: Add** `{{with .BoostedBy}}<span>boosted by {{.}}</span>{{end}}` after the author span.
- [ ] **Step 4: Run** `go test ./internal/sip/web/` — expect PASS with goldens unchanged (no fixture has a boost).
- [ ] **Step 5: Commit** `feat: show who boosted a piece`

---

### Task 5: Wire stw into sip

**Files:**
- Modify: `cmd/sip/edition.go` (`buildEdition`)
- Modify: `cmd/sip/main.go` (flags, `login fediverse`, building the source)
- Modify: `cmd/sip/login.go` (`loginToken`, `promptToken`)
- Test: `cmd/sip/main_test.go`

**Interfaces:**
- Consumes: `stw.Client.Items`, `stw.Item` (Task 2); `piece.FromItem` (Task 3); `account.Secret` (Task 1).
- Produces:

```go
type fediverse func(since, until time.Time) ([]stw.Item, error)
func buildEdition(w windower, fedi fediverse, now time.Time, loc *time.Location, warn io.Writer) (edition.Edition, error)
func loginToken(store interface{ Set(string) error }, readToken func() (string, error)) error
```

- [ ] **Step 1: Write the failing tests** (update the existing `buildEdition` calls to pass a nil-safe fake `fediverse` that returns no items).

- `TestBuildEditionAddsFediverse`: `fakeWindow` with one email arriving 2026-10-15 09:00 UTC; `fedi` returns one item with `ReceivedAt` 2026-10-15 10:00 UTC, records `since`/`until`. With `now` = 2026-10-16 08:00 UTC: `since == edition.Start(cutoff)`, `until == cutoff`, `ed.Count() == 2`, one piece has `Channel == "fediverse"`, `warn` empty.
- `TestBuildEditionFediverseError`: `fedi` returns `errors.New("GET items: 404 Not Found")` → no error, `ed.Count() == 1` (the email), `warn == "sip: fediverse: GET items: 404 Not Found\n"`.
- `TestBuildEditionFediverseSkipped`: `fedi == nil` → `ed.Count() == 1`, `warn == "sip: fediverse skipped: run sip login fediverse\n"`.
- `TestBuildEditionEmailErrorStillFails`: `fakeWindow{err: boom}` → error wraps `boom` and `fedi` is not called.
- `TestLoginToken`: `keyring.MockInit()`; `loginToken(account.Secret{Service: "sip", Name: "stw-reader"}, func() (string, error) { return "tok", nil })` → `Get() == "tok"`; existing IMAP logins under `sip` still load.
- `TestLoginTokenTrims`: input `"  tok\n"` → saved `"tok"`.
- `TestLoginTokenEmpty`: input `"  "` → error containing `token`.

- [ ] **Step 2: Run** `go test ./cmd/sip/` — expect FAIL.
- [ ] **Step 3: Implement.**
  - `buildEdition`: fetch email first (errors stay fatal, unchanged); then if `fedi == nil` print the skipped line, else call it and either print `sip: fediverse: <err>` or append `piece.FromItem` of each item; compose all pieces together.
  - `main.go`: add `-stw` (`SIP_STW_URL`, default `https://ap.nbaglivo.dev`) and `-site` (`SIP_STW_SITE`, default `inner-monologue`); accept `login fediverse` (which does not need `-host`/`-user`) next to `login`; update usage to `usage: sip [-host h] [-port p] [-user u] [-stw url] [-site id] [login [fediverse]]`. In `run`, read `account.Secret{Service: "sip", Name: "stw-reader"}`: empty token → `fedi = nil`; a keychain error → `fedi` returns that error (so it is printed as a `sip: fediverse:` line); else `fedi` calls `stw.Client{BaseURL, Site, Token}.Items(context.Background(), since, until)`.
  - `login.go`: `loginToken` trims spaces, rejects empty with `empty token`, calls `Set`; `promptToken(site, base string)` prompts `Reader token for <site> at <base>: ` with `term.ReadPassword`.
- [ ] **Step 4: Run** `go test ./... && go vet ./...` — expect PASS.
- [ ] **Step 5: Smoke run.** `go run ./cmd/sip -host … -user …` with no token saved prints the skipped line and serves the email edition; with `-stw http://127.0.0.1:9` and a token saved (`sip login fediverse`) prints one `sip: fediverse:` line and still serves the edition.
- [ ] **Step 6: Commit** `feat: add stw fediverse items to the edition`
