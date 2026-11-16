package stw

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var (
	since = time.Date(2026, 10, 15, 4, 30, 0, 0, time.UTC)
	until = since.Add(24 * time.Hour)
)

func serve(t *testing.T, h http.HandlerFunc) Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	return Client{BaseURL: srv.URL, Site: "inner-monologue", Token: "tok"}
}

func page(t *testing.T, name string, cursor any) []byte {
	t.Helper()
	raw, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	m["next_cursor"] = cursor
	out, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func writeJSON(w http.ResponseWriter, body []byte) {
	w.Header().Set("Content-Type", "application/json")
	w.Write(body)
}

func TestItemsRequest(t *testing.T) {
	var got *http.Request
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		got = r
		writeJSON(w, page(t, "caughtup.json", nil))
	})
	loc, err := time.LoadLocation("Europe/Madrid")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Items(t.Context(), since.In(loc), until.In(loc)); err != nil {
		t.Fatal(err)
	}
	if got.URL.Path != "/reader/v1/sites/inner-monologue/items" {
		t.Errorf("path = %q", got.URL.Path)
	}
	q := got.URL.Query()
	want := map[string]string{
		"since": "2026-10-15T04:30:00.000Z",
		"until": "2026-10-16T04:30:00.000Z",
		"order": "asc",
		"limit": "200",
	}
	for k, v := range want {
		if q.Get(k) != v {
			t.Errorf("query %s = %q, want %q", k, q.Get(k), v)
		}
	}
	if q.Has("cursor") {
		t.Errorf("unexpected cursor %q", q.Get("cursor"))
	}
	if h := got.Header.Get("Authorization"); h != "Bearer tok" {
		t.Errorf("Authorization = %q", h)
	}
	if h := got.Header.Get("Accept"); h != "application/json" {
		t.Errorf("Accept = %q", h)
	}
}

func TestItemsPages(t *testing.T) {
	var n atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		if q.Get("since") != "2026-10-15T04:30:00.000Z" || q.Get("until") != "2026-10-16T04:30:00.000Z" || q.Get("order") != "asc" {
			t.Errorf("params = %v", q)
		}
		if n.Add(1) == 1 {
			writeJSON(w, page(t, "morning.json", "c1"))
			return
		}
		if q.Get("cursor") != "c1" {
			t.Errorf("cursor = %q", q.Get("cursor"))
		}
		writeJSON(w, page(t, "caughtup.json", nil))
	})
	items, err := c.Items(t.Context(), since, until)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 2 {
		t.Fatalf("requests = %d, want 2", n.Load())
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2", len(items))
	}
	if items[0].ID != "https://mastodon.social/users/alice/statuses/113284750912345678" {
		t.Errorf("id = %q", items[0].ID)
	}
	if items[1].Boost == nil || items[1].Boost.By.Name != "Carol" {
		t.Errorf("boost = %+v", items[1].Boost)
	}
	if items[0].Attachments[0].Kind != "image" {
		t.Errorf("kind = %q", items[0].Attachments[0].Kind)
	}
	if items[1].Title != "Autumn reading" {
		t.Errorf("title = %q", items[1].Title)
	}
}

func TestItemsStopsOnNullCursor(t *testing.T) {
	var n atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		n.Add(1)
		writeJSON(w, page(t, "morning.json", nil))
	})
	items, err := c.Items(t.Context(), since, until)
	if err != nil {
		t.Fatal(err)
	}
	if n.Load() != 1 || len(items) != 2 {
		t.Fatalf("requests = %d, items = %d", n.Load(), len(items))
	}
}

func TestItemsUnauthorized(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusUnauthorized)
		fmt.Fprint(w, `{"error":"Missing or wrong reader token","code":"unauthorized"}`)
	})
	items, err := c.Items(t.Context(), since, until)
	if err == nil {
		t.Fatal("expected error")
	}
	for _, s := range []string{"401", "unauthorized", "Missing or wrong reader token"} {
		if !strings.Contains(err.Error(), s) {
			t.Errorf("error %q lacks %q", err, s)
		}
	}
	if items != nil {
		t.Errorf("items = %v", items)
	}
}

func TestItemsNotFound(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
	})
	_, err := c.Items(t.Context(), since, until)
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Fatalf("err = %v", err)
	}
}

func TestItemsNotJSON(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		fmt.Fprint(w, "<html>hi</html>")
	})
	items, err := c.Items(t.Context(), since, until)
	if err == nil || items != nil {
		t.Fatalf("items = %v, err = %v", items, err)
	}
}

func TestItemsTimeout(t *testing.T) {
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-time.After(200 * time.Millisecond):
		case <-r.Context().Done():
		}
	})
	c.Timeout = 50 * time.Millisecond
	start := time.Now()
	items, err := c.Items(t.Context(), since, until)
	if err == nil || items != nil {
		t.Fatalf("items = %v, err = %v", items, err)
	}
	if d := time.Since(start); d > 150*time.Millisecond {
		t.Errorf("took %v", d)
	}
}

func TestItemsPageCap(t *testing.T) {
	var n atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		writeJSON(w, page(t, "morning.json", fmt.Sprintf("c%d", n.Add(1))))
	})
	items, err := c.Items(t.Context(), since, until)
	if err == nil || !strings.Contains(err.Error(), "25 pages") {
		t.Fatalf("err = %v", err)
	}
	if items != nil {
		t.Errorf("items = %d", len(items))
	}
	if n.Load() != MaxPages {
		t.Errorf("requests = %d, want %d", n.Load(), MaxPages)
	}
}

func TestItemsDropsPartialPages(t *testing.T) {
	var n atomic.Int32
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		if n.Add(1) == 1 {
			writeJSON(w, page(t, "morning.json", "c1"))
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
	})
	items, err := c.Items(t.Context(), since, until)
	if err == nil || items != nil {
		t.Fatalf("items = %v, err = %v", items, err)
	}
}

func TestItemsTrailingSlash(t *testing.T) {
	var path string
	c := serve(t, func(w http.ResponseWriter, r *http.Request) {
		path = r.URL.Path
		writeJSON(w, page(t, "caughtup.json", nil))
	})
	c.BaseURL += "/"
	if _, err := c.Items(t.Context(), since, until); err != nil {
		t.Fatal(err)
	}
	if path != "/reader/v1/sites/inner-monologue/items" {
		t.Errorf("path = %q", path)
	}
}
