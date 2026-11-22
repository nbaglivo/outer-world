package mastodon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync"
	"testing"
	"time"
)

var since = time.Date(2026, 10, 15, 4, 30, 0, 0, time.UTC)

func serve(t *testing.T, h http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return Client{BaseURL: srv.URL, Token: "tok"}
}

func fixture(t *testing.T) []json.RawMessage {
	t.Helper()
	b, err := os.ReadFile("testdata/home.json")
	if err != nil {
		t.Fatal(err)
	}
	var all []json.RawMessage
	if err := json.Unmarshal(b, &all); err != nil {
		t.Fatal(err)
	}
	return all
}

func writeJSON(t *testing.T, w http.ResponseWriter, v any) {
	t.Helper()
	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(v); err != nil {
		t.Error(err)
	}
}

// pages serves the given pages in order, then [], recording each request.
func pages(t *testing.T, ps ...[]json.RawMessage) (Client, func() []*http.Request) {
	t.Helper()
	var mu sync.Mutex
	var reqs []*http.Request
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		reqs = append(reqs, r.Clone(context.Background()))
		n := len(reqs) - 1
		mu.Unlock()
		if n < len(ps) {
			writeJSON(t, w, ps[n])
			return
		}
		writeJSON(t, w, []json.RawMessage{})
	})
	return c, func() []*http.Request {
		mu.Lock()
		defer mu.Unlock()
		return reqs
	}
}

func TestHomeRequest(t *testing.T) {
	all := fixture(t)
	c, reqs := pages(t, all)
	if _, err := c.Home(context.Background(), since); err != nil {
		t.Fatal(err)
	}
	r := reqs()[0]
	if r.URL.Path != "/api/v1/timelines/home" {
		t.Errorf("path = %q", r.URL.Path)
	}
	if r.URL.RawQuery != "limit=40" {
		t.Errorf("query = %q", r.URL.RawQuery)
	}
	if got := r.Header.Get("Authorization"); got != "Bearer tok" {
		t.Errorf("authorization = %q", got)
	}
	if got := r.Header.Get("Accept"); got != "application/json" {
		t.Errorf("accept = %q", got)
	}
}

func TestHomePagesUntilOlder(t *testing.T) {
	all := fixture(t)
	c, reqs := pages(t, all[:2], all[2:])
	tl, err := c.Home(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	rs := reqs()
	if len(rs) != 2 {
		t.Fatalf("requests = %d, want 2", len(rs))
	}
	q := rs[1].URL.Query()
	if q.Get("max_id") != "200" || q.Get("limit") != "40" {
		t.Errorf("second query = %q", rs[1].URL.RawQuery)
	}
	if len(tl.Statuses) != 3 || !tl.Complete {
		t.Fatalf("statuses = %d, complete = %v", len(tl.Statuses), tl.Complete)
	}
	if got := tl.Statuses[0].Reblog.Account.DisplayName; got != "Bob's notebook" {
		t.Errorf("reblog display name = %q", got)
	}
	if got := tl.Statuses[0].Reblog.Card.URL; got != "https://write.as/bob/autumn-reading" {
		t.Errorf("card url = %q", got)
	}
	if got := tl.Statuses[1].Attachments[0].Type; got != "image" {
		t.Errorf("attachment type = %q", got)
	}
	if s := tl.Statuses[2]; !s.Sensitive || s.SpoilerText != "Spoilers for the last chapter" {
		t.Errorf("status 100 = %+v", s)
	}
	if tl.Statuses[1].Reblog != nil || tl.Statuses[1].Card != nil {
		t.Error("null reblog/card should stay nil")
	}
}

func TestHomeKeepsPagesWithSomeNewer(t *testing.T) {
	all := fixture(t)
	c, reqs := pages(t, []json.RawMessage{all[1], all[2]})
	tl, err := c.Home(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs()) != 2 {
		t.Errorf("requests = %d, want 2", len(reqs()))
	}
	if len(tl.Statuses) != 2 || tl.Complete {
		t.Errorf("statuses = %d, complete = %v", len(tl.Statuses), tl.Complete)
	}
}

func TestHomeLateBoostDoesNotStop(t *testing.T) {
	all := fixture(t)
	c, reqs := pages(t, all[:1], []json.RawMessage{})
	tl, err := c.Home(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs()) != 2 {
		t.Errorf("requests = %d, want 2", len(reqs()))
	}
	if len(tl.Statuses) != 1 || tl.Complete {
		t.Errorf("statuses = %d, complete = %v", len(tl.Statuses), tl.Complete)
	}
}

func TestHomeShortTimeline(t *testing.T) {
	all := fixture(t)
	c, _ := pages(t, all[:2], []json.RawMessage{})
	tl, err := c.Home(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	want := time.Date(2026, 10, 15, 9, 0, 0, 0, time.UTC)
	if tl.Complete || len(tl.Statuses) != 2 || !tl.Oldest.Equal(want) {
		t.Errorf("complete=%v statuses=%d oldest=%v", tl.Complete, len(tl.Statuses), tl.Oldest)
	}
}

func TestHomeEmpty(t *testing.T) {
	c, _ := pages(t, []json.RawMessage{})
	tl, err := c.Home(context.Background(), since)
	if err != nil {
		t.Fatal(err)
	}
	if tl.Complete || !tl.Oldest.IsZero() || len(tl.Statuses) != 0 {
		t.Errorf("timeline = %+v", tl)
	}
}

func TestHomeUnauthorized(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"The access token is invalid"}`)
	})
	tl, err := c.Home(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "401") || !strings.Contains(err.Error(), "The access token is invalid") {
		t.Errorf("err = %v", err)
	}
	if len(tl.Statuses) != 0 {
		t.Errorf("timeline = %+v", tl)
	}
}

func TestHomePartialContent(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusPartialContent)
		fmt.Fprint(w, `[]`)
	})
	_, err := c.Home(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "regenerating") {
		t.Errorf("err = %v", err)
	}
}

func TestHomeNotFound(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.Home(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("err = %v", err)
	}
}

func TestHomeNotJSON(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>hi</html>")
	})
	if _, err := c.Home(context.Background(), since); err == nil {
		t.Error("want error")
	}
}

func TestHomeTimeout(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		time.Sleep(200 * time.Millisecond)
		writeJSON(t, w, []json.RawMessage{})
	})
	c.Timeout = 50 * time.Millisecond
	_, err := c.Home(context.Background(), since)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v, want deadline exceeded", err)
	}
}

func TestHomePageCap(t *testing.T) {
	var mu sync.Mutex
	n := 0
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		n++
		id := n
		mu.Unlock()
		writeJSON(t, w, []map[string]any{{
			"id":         fmt.Sprint(1000 - id),
			"created_at": "2026-10-16T00:00:00Z",
		}})
	})
	tl, err := c.Home(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "25 pages") {
		t.Errorf("err = %v", err)
	}
	if n != MaxPages {
		t.Errorf("requests = %d, want %d", n, MaxPages)
	}
	if len(tl.Statuses) != 0 {
		t.Errorf("statuses = %d", len(tl.Statuses))
	}
}

func TestHomeDropsPartialPages(t *testing.T) {
	all := fixture(t)
	var n int
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		n++
		if n == 1 {
			writeJSON(t, w, all[:2])
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	tl, err := c.Home(context.Background(), since)
	if err == nil || !strings.Contains(err.Error(), "500") {
		t.Errorf("err = %v", err)
	}
	if len(tl.Statuses) != 0 || tl.Complete || !tl.Oldest.IsZero() {
		t.Errorf("timeline = %+v", tl)
	}
}

func TestHomeTrailingSlash(t *testing.T) {
	var path string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(t, w, []json.RawMessage{})
	}))
	t.Cleanup(srv.Close)
	c := Client{BaseURL: srv.URL + "/", Token: "tok"}
	if _, err := c.Home(context.Background(), since); err != nil {
		t.Fatal(err)
	}
	if path != "/api/v1/timelines/home" {
		t.Errorf("path = %q", path)
	}
}
