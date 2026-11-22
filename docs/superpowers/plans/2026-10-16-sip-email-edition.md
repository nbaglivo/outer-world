# One sip at a time: monorepo and email edition Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn the nmail repo into a two-app monorepo (milestone 1) and ship `sip`, which builds today's edition from subs@nbaglivo.dev and serves it as a broadsheet in the browser (milestone 2).

**Architecture:** `cmd/nmail` and `cmd/sip` are two binaries in the one `nmail` module, sharing `internal/{account,mailbox,render}`. sip reads the window's mail read-only (`mailbox.Session.Window`), turns each message into a `piece.Piece`, arranges them with the pure `edition.Compose`, and serves `html/template` pages from `internal/sip/web` on 127.0.0.1 with embedded CSS and fonts.

**Tech Stack:** Go 1.27 (mise), go-imap v2 (+ `imapmemserver` for tests), go-message, go-keyring, `golang.org/x/net/html`, `github.com/microcosm-cc/bluemonday` v1.0.27, `golang.org/x/term` v0.47.0, `html/template`, `embed`, Newsreader and IBM Plex Mono from `@fontsource` 5.3.0.

**Spec:** `docs/superpowers/specs/2026-10-16-one-sip-at-a-time-design.md`

## Global Constraints

- Module path stays `nmail`. Entrypoints: `cmd/nmail/main.go`, `cmd/sip/main.go`.
- nmail's behaviour and tests stay as they are; `go build ./cmd/nmail` must produce the same app. Saved nmail logins (keychain service `nmail`, user `default`) keep working.
- IMAP is read-only for sip: `Select` with `ReadOnly: true`, body fetches with `Peek: true`. No marking seen, deleting or moving.
- sip env/flags: `-host`/`SIP_HOST`, `-port`/`SIP_PORT` (default `993`), `-user`/`SIP_USER`; password from keychain service `sip`.
- Cutoff is 06:30 local; window is `[previous cutoff, cutoff)` on the IMAP internal date (arrival). DST: compute 06:30 on the calendar day.
- Minutes = ceil(words / 230), minimum 1. Longer reads: `Minutes ≥ 3`. Lead: most words, ties newest.
- Front page: no source labels. Byline only on the opened piece.
- Palette: paper `#faf8f4`, ink `#38352f`, headline `#201e1a`, muted `#6f6a60`, meta `#756e60` / `#8a857b`, hairline `#ebe6dc`. Newsreader for headlines and body (300 for masthead and opened title), IBM Plex Mono for metadata; fonts embedded, never loaded from Google.
- Empty edition copy, exactly: `Nothing new since yesterday morning. Enjoy the coffee.`
- Conventional, lowercase commit messages ending with `Co-Authored-By: Claude Opus 5.5 <noreply@anthropic.com>`. Never push.

## Review Focus

1. **Window with no mail** (quiet day): `Window` must return an empty slice, not an IMAP error from fetching an empty set → test in Task 3.
2. **Mail that arrived just before the window opened but on the same calendar day** (SINCE is day-granular): it must be filtered out by internal date → test in Task 3.
3. **A newsletter with no subject, or a subject that is only list tags** (`[Weekly]`): the front page must still show a headline, falling back to the dek → test in Task 5.
4. **Hostile or messy HTML** (scripts, `on*` handlers, `javascript:` links, hidden preheader, 1×1 pixels): none of it reaches the page, and the preheader becomes the dek rather than body text → tests in Task 5.
5. **Running `sip` before 06:30 or on the day after a skipped day**: the edition is the one ending at the latest 06:30, covering exactly 24 h (23/25 on DST days) → tests in Task 4 and Task 8.

---

## Milestone 1: monorepo

### Task 1: keychain service as a parameter

**Files:**
- Modify: `internal/account/account.go`
- Test: `internal/account/account_test.go`

**Interfaces:**
- Produces: `type Keyring struct{ Service string }`; empty `Service` means `"nmail"`. `Load`/`Save` keep their signatures and the keychain user `"default"`.

- [ ] **Step 1: Write the failing test**

```go
func TestServicesAreSeparate(t *testing.T) {
	keyring.MockInit()
	nm, sip := Keyring{}, Keyring{Service: "sip"}
	a := []mailbox.Credentials{{Host: "h", Port: "993", Username: "me", Password: "a"}}
	if err := sip.Save(a); err != nil {
		t.Fatal(err)
	}
	if list, _ := nm.Load(); len(list) != 0 {
		t.Fatalf("nmail sees sip's logins: %+v", list)
	}
	if got, _ := (Keyring{Service: "nmail"}).Load(); len(got) != 0 {
		t.Fatal("explicit nmail service differs from the default")
	}
	if got, _ := sip.Load(); !slices.Equal(got, a) {
		t.Fatalf("sip load = %+v", got)
	}
}
```

Keep `TestLoadSingleLoginFormat` passing: it writes under `"nmail"`/`"default"` directly, so rename the old `service` const to `defaultService = "nmail"` and update that test's reference.

- [ ] **Step 2: Run** `go test ./internal/account/` — Expected: FAIL (`unknown field Service`).
- [ ] **Step 3: Implement** a `func (k Keyring) service() string` returning `k.Service` or `defaultService`; switch `Load`/`Save` to value receivers using it.
- [ ] **Step 4: Run** `go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `refactor: make the keychain service a keyring field`

### Task 2: move nmail to cmd/nmail

**Files:**
- Move: `main.go` → `cmd/nmail/main.go` (`git mv`)
- Modify: `.gitignore` (add `/sip` next to `/nmail`)

- [ ] **Step 1:** `git mv main.go cmd/nmail/main.go`; set `account.Keyring{Service: "nmail"}` there. No other change.
- [ ] **Step 2: Verify**: `go vet ./... && go test ./... && go build -o /tmp/nmail-check ./cmd/nmail && /tmp/nmail-check -h` — Expected: tests PASS, help lists `-host -port -user -open-with` exactly as before.
- [ ] **Step 3: Commit** `refactor: move nmail into cmd/nmail`

---

## Milestone 2: email edition in the browser

### Task 3: `mailbox.Session.Window`

**Files:**
- Modify: `internal/mailbox/imap.go`
- Test: `internal/mailbox/imap_test.go`

**Interfaces:**
- Produces:
  - `Message` gains `Arrived time.Time` (IMAP internal date), `FromAddr string` (first From address, `user@domain`), `MessageID string` (envelope, without `<>`), `ListID string` (raw `List-Id` header value), `ListPost string` (raw `List-Post` value). `Read` fills them too.
  - `func (s *Session) Window(from, to time.Time) ([]*Message, error)`: INBOX messages with `from ≤ Arrived < to`, oldest first, read-only.

- [ ] **Step 1: Write the failing tests.** Add a helper `appendAt(t, s, raw string, at time.Time)` that appends with `&imap.AppendOptions{Time: at}`. Use a session with no default messages (give `newTestSession` an option or add `newEmptySession`).

```go
func TestWindow(t *testing.T) {
	s := newEmptySession(t)
	day := time.Date(2026, 10, 16, 6, 30, 0, 0, time.UTC)
	appendAt(t, s, listMsg("before"), day.Add(-24*time.Hour-time.Minute)) // same calendar day as start, too early
	appendAt(t, s, listMsg("first"), day.Add(-24*time.Hour))              // exactly at start: in
	appendAt(t, s, listMsg("second"), day.Add(-time.Minute))
	appendAt(t, s, listMsg("at-cutoff"), day)                             // at end: out
	got, err := s.Window(day.Add(-24*time.Hour), day)
	// want subjects ["first", "second"], Arrived equal to the append times,
	// ListID "Weekly <weekly.example.com>", ListPost "<https://example.com/p/1>",
	// MessageID "first@example.com", FromAddr "news@example.com", Raw non-empty.
}

func TestWindowEmpty(t *testing.T) {
	// window with no messages → len 0, err nil
}

func TestWindowIsReadOnly(t *testing.T) {
	// after Window, Inbox(10) reports Seen == false for every message
}
```

`listMsg(subject)` builds a message with `From: Weekly <news@example.com>`, `Message-ID: <subject@example.com>`, `List-Id: Weekly <weekly.example.com>`, `List-Post: <https://example.com/p/1>`.

- [ ] **Step 2: Run** `go test ./internal/mailbox/ -run Window` — Expected: FAIL (`s.Window undefined`).
- [ ] **Step 3: Implement.** Select INBOX `ReadOnly: true`; `UIDSearch` with `SearchCriteria{Since: from}` (day granularity); return `nil, nil` on no UIDs; fetch `UID, Envelope, Flags, InternalDate` plus a `Peek` body section; build each `Message` like `Read` does (factor the shared part into `func message(b *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection) (*Message, error)`); keep those with `from ≤ InternalDate < to`; sort by `Arrived`. Read `List-Id`/`List-Post` from the parsed header in `extractParts` (return them, or add a small `headers(raw)` helper using `mail.CreateReader`).
- [ ] **Step 4: Run** `go test ./...` — Expected: PASS (nmail's tests untouched).
- [ ] **Step 5: Commit** `feat: read a mailbox window by arrival time`

### Task 4: `edition.Cutoff` and `edition.Start`

**Files:**
- Create: `internal/sip/edition/cutoff.go`
- Test: `internal/sip/edition/cutoff_test.go`

**Interfaces:**
- Produces:
  - `func Cutoff(now time.Time, loc *time.Location) time.Time`: the latest 06:30 in `loc` at or before `now`.
  - `func Start(cutoff time.Time) time.Time`: the 06:30 on the calendar day before `cutoff` (`Cutoff(cutoff.Add(-time.Nanosecond), cutoff.Location())`).

- [ ] **Step 1: Write the failing table test** `TestCutoff` with `loc, _ := time.LoadLocation("Europe/Madrid")`:

| now (Madrid) | Cutoff | Start |
|---|---|---|
| 2026-10-16 09:00 | 2026-10-16 06:30 | 2026-10-15 06:30 |
| 2026-10-16 06:30:00 | 2026-10-16 06:30 | 2026-10-15 06:30 |
| 2026-10-16 06:29:59 | 2026-10-15 06:30 | 2026-10-14 06:30 |
| 2026-10-16 00:10 | 2026-10-15 06:30 | 2026-10-14 06:30 |
| 2026-10-25 12:00 (DST ends) | 2026-10-25 06:30 | 2026-10-24 06:30, and `cutoff.Sub(start) == 25*time.Hour` |
| 2026-03-29 12:00 (DST starts) | 2026-03-29 06:30 | 2026-03-28 06:30, and `cutoff.Sub(start) == 23*time.Hour` |

Also assert `Cutoff` of a `now` given in UTC returns a time whose `Location()` is `loc`.

- [ ] **Step 2: Run** `go test ./internal/sip/edition/` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement** with `time.Date(y, m, d, 6, 30, 0, 0, loc)` on `now.In(loc)`'s date, stepping back one calendar day when `now` is before it.
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat: add the 06:30 edition cutoff`

### Task 5: `piece.Piece` and email HTML cleaning

**Files:**
- Create: `internal/sip/piece/piece.go` (type, `Minutes`, `Headline`), `internal/sip/piece/clean.go`
- Test: `internal/sip/piece/clean_test.go`, `internal/sip/piece/piece_test.go`
- Modify: `go.mod`/`go.sum` (`go get github.com/microcosm-cc/bluemonday@v1.0.27`)

**Interfaces:**
- Produces:
  ```go
  type Piece struct {
  	ID, Title, Dek, Author, Source, Channel string
  	Published, Arrived time.Time
  	HTML  template.HTML // cleaned, safe to emit
  	Text  string
  	Words int
  	Original string // http(s) URL or ""
  	Raw   []byte
  }
  func (p Piece) Minutes() int     // ceil(Words/230), min 1
  func (p Piece) Headline() string // Title, else Dek, else "Untitled"
  type Cleaned struct {
  	HTML      template.HTML
  	Preheader string // text of the first hidden element with text, trimmed
  	ViewLink  string // href of a "view in browser"/"view online"/"read online" link, http(s) only
  }
  func Clean(src string) Cleaned
  ```

- [ ] **Step 1: Write the failing tests**

```go
func TestCleanStripsNoise(t *testing.T) {
	c := Clean(`<html><head><style>p{}</style><script>x()</script></head><body>
<div style="display:none">Inside: three essays on slow software</div>
<p><a href="https://ex.com/web">View in browser</a></p>
<h1 class="h" style="color:red" onclick="x()">Notes</h1>
<p>Read <a href="https://ex.com/a">this</a> and <a href="javascript:alert(1)">that</a>.</p>
<img src="https://ex.com/pic.jpg" alt="A cup"><img src="https://t.ex.com/o.gif" width="1" height="1">
<table><tr><td><p>Cell text</p></td></tr></table></body></html>`)
	// c.Preheader == "Inside: three essays on slow software"
	// c.ViewLink == "https://ex.com/web"
	// HTML contains `<h1>Notes</h1>`, `href="https://ex.com/a"`, `src="https://ex.com/pic.jpg"`, `alt="A cup"`, "Cell text"
	// HTML does not contain: "<script", "<style", "style=", "class=", "onclick", "javascript:", "o.gif", "<table", "<td", "three essays", "View in browser"
}

func TestMinutes(t *testing.T)  { /* Words 0→1, 230→1, 231→2, 690→3 */ }
func TestHeadline(t *testing.T) { /* Title "X"→"X"; Title ""+Dek "D"→"D"; both ""→"Untitled" */ }
```

- [ ] **Step 2: Run** `go test ./internal/sip/piece/` — Expected: FAIL (undefined).
- [ ] **Step 3: Implement `Clean`** in two passes: (1) parse with `golang.org/x/net/html`, record the preheader and view link, then remove hidden elements (`display:none`, `visibility:hidden`, `hidden` attr, `max-height:0`/`font-size:0`), images with `width` or `height` ≤ 2, and the view-link anchor's paragraph; (2) render and run a bluemonday policy allowing `h1–h6 p br hr ul ol li blockquote pre code em strong b i a img figure figcaption`, `href` (http, https, mailto) on `a` with `AddTargetBlankToFullyQualifiedLinks(true)` and `RequireNoReferrerOnLinks(true)`, `src` (http, https) and `alt` on `img`. Tables and other disallowed tags drop and keep their contents (bluemonday's default).
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat: add the piece and clean newsletter html`

### Task 6: email → Piece

**Files:**
- Create: `internal/sip/piece/email.go`, `internal/sip/piece/testdata/{plain.eml,html-preheader.eml,list-id.eml}`
- Modify: `internal/mailbox/imap.go` (add `Parse`)
- Test: `internal/sip/piece/email_test.go` (also loads `../../render/testdata/digest.html` wrapped as an HTML-only message)

**Interfaces:**
- Consumes: `mailbox.Message` fields from Task 3.
- Produces: `func FromEmail(m *mailbox.Message) Piece`, and in `internal/mailbox` `func Parse(raw []byte) (*Message, error)` (envelope fields, parts and list headers from raw bytes; `Arrived` and `UID` left zero for the caller), so tests build messages from the `.eml` fixtures and set `Arrived` themselves.

Rules (from the spec): ID = MessageID else `uid:<UID>`; Title = Subject with leading `[...]` tags removed (`^(\s*\[[^\]]*\]\s*)+`); Author = From display name else `FromAddr`; Source = List-Id description (the part before `<`, trimmed, unquoted) else From display name else the domain of `FromAddr`; Channel `email`; Published = envelope Date else Arrived; Arrived = `m.Arrived`; HTML = `Clean(m.HTML).HTML`, or for text-only mail the text split on blank lines into escaped `<p>` paragraphs; Text = `m.Text`, else `m.Body` from HTML; Words = `len(strings.Fields(Text))`; Dek = preheader, else first sentence of Text (up to the first `. `, `! `, `? ` or newline), cut at 160 chars on a word boundary with `…`; Original = `List-Post` when it is `<http…>`, else `ViewLink`, else ""; Raw = `m.Raw`.

- [ ] **Step 1: Write the failing tests**, one per fixture:
  - `plain.eml` (Subject `[Weekly] Notes on slow software`, From `Robin <robin@example.com>`, no List-Id, 700 words of text/plain): Title `Notes on slow software`, Author and Source `Robin`, Minutes 4, HTML starts with `<p>`, Dek is its first sentence.
  - `html-preheader.eml` (From `news@letters.example.org` with no name, HTML only, hidden preheader `Rates, again, but calmly.`, a 1×1 pixel): Author `news@letters.example.org`, Source `letters.example.org`, Dek `Rates, again, but calmly.`, HTML lacks the pixel URL, Text non-empty.
  - `list-id.eml` (`List-Id: "Money Stuff" <money.example.com>`, `List-Post: <https://example.com/p/9>`, no Date header): Source `Money Stuff`, Original `https://example.com/p/9`, Published equals Arrived.
  - digest: Title from the Summary subject, Words > 0, HTML contains `Confirm your subscription`.
  - Subject `[Weekly]` only and empty subject: Title `""` and `Headline()` equals the Dek.
  - A Dek built from a 300-char first sentence is ≤ 161 runes and ends with `…`.
- [ ] **Step 2: Run** `go test ./internal/sip/piece/` — Expected: FAIL.
- [ ] **Step 3: Implement** `FromEmail` and `mailbox.Parse`.
- [ ] **Step 4: Run** `go test ./...` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: turn a newsletter into a piece`

### Task 7: `edition.Compose` and the Ranker

**Files:**
- Create: `internal/sip/edition/edition.go`
- Test: `internal/sip/edition/edition_test.go`

**Interfaces:**
- Consumes: `piece.Piece`, `Start` from Task 4.
- Produces:
  ```go
  type Ranker interface{ Rank([]piece.Piece) []piece.Piece } // first element is the lead
  type ByLength struct{}                                    // newest first, then the most-words piece (ties: newest) moved to the front
  type Edition struct {
  	Cutoff  time.Time
  	Lead    *piece.Piece
  	Longer  []piece.Piece
  	Briefs  []piece.Piece
  }
  func Compose(pieces []piece.Piece, cutoff time.Time) Edition // ComposeWith(…, ByLength{})
  func ComposeWith(pieces []piece.Piece, cutoff time.Time, r Ranker) Edition
  func (e Edition) Order() []piece.Piece // lead, longer…, briefs…: front-page and "next piece" order
  func (e Edition) Count() int
  func (e Edition) Minutes() int         // sum of Minutes() over Order()
  func (e Edition) Empty() bool
  ```

- [ ] **Step 1: Write the failing tests**
  - `TestComposeWindow`: cutoff 2026-10-16 06:30 Madrid; pieces arriving at Start (in), cutoff − 1s (in), cutoff (out), Start − 1s (out) → `Count() == 2`.
  - `TestComposeLead`: words 100/900/900 with the later 900 newer → lead is the newer 900.
  - `TestComposeSplit`: Minutes 2, 3, 5 (non-lead) → Longer has the 3 and 5 (newest first), Briefs has the 2.
  - `TestComposeEmpty`: no pieces → `Empty()`, `Lead == nil`, `Count() == 0`, `Minutes() == 0`.
  - `TestComposeWithRanker`: a fake Ranker that reverses input → its first element is the lead.
- [ ] **Step 2: Run** `go test ./internal/sip/edition/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Run** — Expected: PASS.
- [ ] **Step 5: Commit** `feat: compose the daily edition`

### Task 8: web pages

**Files:**
- Create: `internal/sip/web/web.go`, `internal/sip/web/templates/{layout.html,front.html,piece.html}`, `internal/sip/web/assets/{style.css,mark.svg}`, `internal/sip/web/assets/fonts/{newsreader-300.woff2,newsreader-400.woff2,newsreader-400-italic.woff2,plex-mono-400.woff2,OFL-newsreader.txt,OFL-plex-mono.txt}`
- Test: `internal/sip/web/web_test.go`, `internal/sip/web/testdata/{front.golden.html,piece.golden.html,empty.golden.html}`

**Interfaces:**
- Consumes: `edition.Edition` and its methods (Task 7), `piece.Piece.Headline/Minutes` (Task 5).
- Produces: `func Handler(ed edition.Edition, loc *time.Location) http.Handler` serving `GET /`, `GET /p/{n}` (1-based index into `ed.Order()`, 404 otherwise), `GET /assets/…` (embedded).

Fonts: download from `https://cdn.jsdelivr.net/npm/@fontsource/newsreader@5.3.0/files/newsreader-latin-{300-normal,400-normal,400-italic}.woff2`, `https://cdn.jsdelivr.net/npm/@fontsource/ibm-plex-mono@5.3.0/files/ibm-plex-mono-latin-400-normal.woff2`, and each package's `LICENSE` as the OFL files.

Copy fixed by the spec and mockup: masthead title `One sip at a time`; date `Monday, 2 January 2006` layout in `loc`; right of it `<n> pieces · ~<m> min` (`1 piece`); kickers `Lead`, `Longer reads`, `Briefs`; brief text is the first ~280 chars of the Dek-or-Text on a word boundary; piece page back link `← One sip at a time`; byline `By <Author>` · `<Source> · <Channel>` · `Mon 2 Jan, 15:04` (Published in `loc`) · `<n> min`; end links `open original ↗` (only when Original) and `next piece →` (only when not last); the empty copy from Global Constraints. The mark is the B4 SVG from the spec, inlined in the masthead. Layout: 1.6fr : 1fr grid, hairline between, single column below 48rem; body measure ~65ch.

- [ ] **Step 1: Write the failing tests**, with a fixture edition of a lead, two longer reads and three briefs built in the test (fixed times in Europe/Madrid), a `-update` flag that rewrites goldens:
  - `TestFrontGolden`, `TestPieceGolden` (`/p/2`), `TestEmptyGolden`.
  - `TestFrontHasNoSources`: front page body contains none of the fixture `Source` or `Author` strings.
  - `TestPieceNotFound`: `/p/0`, `/p/7`, `/p/x` → 404.
  - `TestNextLink`: `/p/6` (last) has no `next piece`; `/p/1` links `/p/2`.
  - `TestAssets`: `/assets/style.css` 200 `text/css`; `/assets/fonts/newsreader-300.woff2` 200.
  - `TestNoGoogleFonts`: no response body contains `fonts.googleapis.com`.
- [ ] **Step 2: Run** `go test ./internal/sip/web/` — Expected: FAIL.
- [ ] **Step 3: Implement** templates, CSS and `Handler` (`embed.FS`, `http.ServeMux` patterns, template funcs for dates and excerpts). Then `go test ./internal/sip/web/ -update` once the pages look right; open the goldens in a browser to check them against the approved mockup.
- [ ] **Step 4: Run** `go test ./internal/sip/web/` — Expected: PASS.
- [ ] **Step 5: Commit** `feat: render the edition as html pages`

### Task 9: the `sip` command

**Files:**
- Create: `cmd/sip/main.go`, `cmd/sip/edition.go`, `cmd/sip/login.go`, `internal/sip/web/serve.go`
- Test: `cmd/sip/main_test.go`
- Modify: `go.mod`/`go.sum` (`go get golang.org/x/term@v0.47.0`)

**Interfaces:**
- Consumes: `mailbox.Connect`, `Session.Window`, `account.Keyring{Service: "sip"}`, `edition.Cutoff/Start/Compose`, `piece.FromEmail`, `web.Handler`.
- Produces:
  - `type windower interface{ Window(from, to time.Time) ([]*mailbox.Message, error) }`
  - `func buildEdition(w windower, now time.Time, loc *time.Location) (edition.Edition, error)`
  - `func login(store interface{ Save([]mailbox.Credentials) error }, c mailbox.Credentials, readPassword func() (string, error)) error`
  - `func web.Serve(ctx context.Context, h http.Handler) (url string, wait func() error, err error)`: listens on `127.0.0.1:0`.

Behaviour: `sip [-host -port -user]` loads the saved login matching host and user from `Keyring{Service: "sip"}` (missing → exit 1 with `no saved password for <user>@<host>; run sip login`), connects, builds, serves, prints the URL, opens it with `open` (macOS), and runs until Ctrl-C. `sip login` prompts `Password for <user>@<host>: ` with no echo (`term.ReadPassword`) and saves the one login. Missing host or user → exit 2 with usage.

- [ ] **Step 1: Write the failing tests**
  - `TestBuildEditionWindow`: fake windower records `from, to`; `now` 2026-10-16 05:50 Madrid → `from` 2026-10-14 06:30, `to` 2026-10-15 06:30; returned edition's Count equals the fake messages.
  - `TestBuildEditionError`: windower error is returned wrapped.
  - `TestLogin`: `keyring.MockInit()`; `login(account.Keyring{Service: "sip"}, c, func() (string, error) { return "pw", nil })` → `Load` returns `c` with `Password "pw"`; nmail's `Keyring{}` still empty; an empty password returns an error and saves nothing.
  - `TestServe`: `web.Serve` with a trivial handler returns a `http://127.0.0.1:` URL that answers 200; cancelling ctx makes `wait()` return nil.
- [ ] **Step 2: Run** `go test ./cmd/sip/ ./internal/sip/web/` — Expected: FAIL.
- [ ] **Step 3: Implement.**
- [ ] **Step 4: Verify** `go vet ./... && go test ./... && go build ./cmd/sip ./cmd/nmail` — Expected: PASS and two binaries. Manual check against the real mailbox (the user runs it): `SIP_HOST=… SIP_USER=subs@nbaglivo.dev ./sip login`, then `./sip` opens today's edition.
- [ ] **Step 5: Commit** `feat: add the sip command`
