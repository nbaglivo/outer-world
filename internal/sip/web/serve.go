package web

import (
	"context"
	"errors"
	"net"
	"net/http"
	"strconv"
	"time"
)

// Serve serves h on a free local port, refusing requests addressed to any
// other host so that a web page can't reach it by DNS rebinding. wait blocks
// until ctx is cancelled, then shuts the server down and returns nil, or
// returns the serve error.
func Serve(ctx context.Context, h http.Handler) (url string, wait func() error, err error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	port := strconv.Itoa(ln.Addr().(*net.TCPAddr).Port)
	local := map[string]bool{"127.0.0.1:" + port: true, "localhost:" + port: true}
	srv := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !local[r.Host] {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		h.ServeHTTP(w, r)
	})}
	failed := make(chan error, 1)
	go func() { failed <- srv.Serve(ln) }()

	wait = func() error {
		select {
		case err := <-failed:
			return err
		case <-ctx.Done():
		}
		shutdown, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		if err := srv.Shutdown(shutdown); err != nil {
			return err
		}
		if err := <-failed; !errors.Is(err, http.ErrServerClosed) {
			return err
		}
		return nil
	}
	return "http://" + ln.Addr().String(), wait, nil
}
