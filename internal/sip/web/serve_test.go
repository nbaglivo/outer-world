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
