package web

import (
	"context"
	"net/http"
	"strings"
	"testing"
)

func TestServe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {})
	url, wait, err := Serve(ctx, h)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(url, "http://127.0.0.1:") {
		t.Fatalf("url = %q", url)
	}
	resp, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d", resp.StatusCode)
	}

	cancel()
	if err := wait(); err != nil {
		t.Fatalf("wait = %v", err)
	}
}

func TestServeRejectsForeignHost(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	url, _, err := Serve(ctx, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	if err != nil {
		t.Fatal(err)
	}
	port := url[strings.LastIndex(url, ":")+1:]
	for host, want := range map[string]int{
		"127.0.0.1:" + port:    http.StatusOK,
		"localhost:" + port:    http.StatusOK,
		"evil.example:" + port: http.StatusForbidden,
		"localhost":            http.StatusForbidden,
		"127.0.0.1:1":          http.StatusForbidden,
	} {
		req, err := http.NewRequest(http.MethodGet, url, nil)
		if err != nil {
			t.Fatal(err)
		}
		req.Host = host
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != want {
			t.Errorf("Host %q: status = %d, want %d", host, resp.StatusCode, want)
		}
	}
}
