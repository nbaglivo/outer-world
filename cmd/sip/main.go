package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"time"

	"nmail/internal/account"
	"nmail/internal/mailbox"
	"nmail/internal/sip/mastodon"
	"nmail/internal/sip/piece"
	"nmail/internal/sip/web"
)

func main() {
	var c mailbox.Credentials
	fs := flag.NewFlagSet("sip", flag.ExitOnError)
	fs.StringVar(&c.Host, "host", envOr("SIP_HOST", ""), "IMAP server address (env SIP_HOST)")
	fs.StringVar(&c.Port, "port", envOr("SIP_PORT", "993"), "IMAP TLS port (env SIP_PORT)")
	fs.StringVar(&c.Username, "user", envOr("SIP_USER", ""), "login username (env SIP_USER)")
	var mastodonURL string
	fs.StringVar(&mastodonURL, "mastodon", envOr("SIP_MASTODON_URL", "https://mastodon.social"), "Mastodon server URL (env SIP_MASTODON_URL)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sip [-host h] [-port p] [-user u] [-mastodon url] [login [mastodon]]")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	args := fs.Args()
	isMastodonLogin := len(args) == 2 && args[0] == "login" && args[1] == "mastodon"
	validArgs := len(args) == 0 || (len(args) == 1 && args[0] == "login") || isMastodonLogin
	if !validArgs {
		fs.Usage()
		os.Exit(2)
	}
	if !isMastodonLogin && (c.Host == "" || c.Username == "") {
		fs.Usage()
		os.Exit(2)
	}

	store := account.Keyring{Service: "sip"}
	var err error
	switch {
	case isMastodonLogin:
		err = loginToken(mastodonSecret, promptToken(mastodonURL))
	case len(args) == 1:
		err = login(store, c, promptPassword(c))
	default:
		err = run(store, c, mastodonURL)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

var mastodonSecret = account.Secret{Service: "sip", Name: "mastodon-token"}

func mastodonFeedFor(base string, loc *time.Location) feed {
	token, err := mastodonSecret.Get()
	switch {
	case err != nil:
		return feed{name: "mastodon", fetch: func(time.Time, time.Time, func(string)) ([]piece.Piece, error) {
			return nil, err
		}}
	case token == "":
		return feed{name: "mastodon"}
	}
	return mastodonFeed(mastodon.Client{BaseURL: base, Token: token}.Home, loc)
}

func run(store account.Keyring, c mailbox.Credentials, mastodonURL string) error {
	saved, err := store.Load()
	if err != nil {
		return err
	}
	found := false
	for _, s := range saved {
		if s.Host == c.Host && s.Username == c.Username {
			c, found = s, true
			break
		}
	}
	if !found {
		return fmt.Errorf("no saved password for %s@%s; run sip login", c.Username, c.Host)
	}

	loc := time.Local
	session, err := mailbox.Connect(c)
	if err != nil {
		return err
	}
	ed, err := buildEdition(session, []feed{mastodonFeedFor(mastodonURL, loc)}, time.Now(), loc, os.Stderr)
	session.Close()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	url, wait, err := web.Serve(ctx, web.Handler(ed, loc))
	if err != nil {
		return err
	}
	fmt.Println(url)
	exec.Command("open", url).Start()
	return wait()
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
