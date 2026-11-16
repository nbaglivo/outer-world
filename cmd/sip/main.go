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
	"nmail/internal/sip/stw"
	"nmail/internal/sip/web"
)

func main() {
	var c mailbox.Credentials
	fs := flag.NewFlagSet("sip", flag.ExitOnError)
	fs.StringVar(&c.Host, "host", envOr("SIP_HOST", ""), "IMAP server address (env SIP_HOST)")
	fs.StringVar(&c.Port, "port", envOr("SIP_PORT", "993"), "IMAP TLS port (env SIP_PORT)")
	fs.StringVar(&c.Username, "user", envOr("SIP_USER", ""), "login username (env SIP_USER)")
	var stwURL, site string
	fs.StringVar(&stwURL, "stw", envOr("SIP_STW_URL", "https://ap.nbaglivo.dev"), "stw server address (env SIP_STW_URL)")
	fs.StringVar(&site, "site", envOr("SIP_STW_SITE", "inner-monologue"), "stw site id (env SIP_STW_SITE)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sip [-host h] [-port p] [-user u] [-stw url] [-site id] [login [fediverse]]")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	args := fs.Args()
	fediLogin := len(args) == 2 && args[0] == "login" && args[1] == "fediverse"
	if len(args) > 2 || (len(args) == 2 && !fediLogin) || (len(args) == 1 && args[0] != "login") {
		fs.Usage()
		os.Exit(2)
	}
	if !fediLogin && (c.Host == "" || c.Username == "") {
		fs.Usage()
		os.Exit(2)
	}

	store := account.Keyring{Service: "sip"}
	token := account.Secret{Service: "sip", Name: "stw-reader"}
	var err error
	switch {
	case fediLogin:
		err = loginToken(token, promptToken(site, stwURL))
	case len(args) == 1:
		err = login(store, c, promptPassword(c))
	default:
		err = run(store, c, token, stwURL, site)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(store account.Keyring, c mailbox.Credentials, token account.Secret, stwURL, site string) error {
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

	session, err := mailbox.Connect(c)
	if err != nil {
		return err
	}
	var fedi fediverse
	tok, tokErr := token.Get()
	switch {
	case tokErr != nil:
		fedi = func(since, until time.Time) ([]stw.Item, error) { return nil, tokErr }
	case tok != "":
		client := stw.Client{BaseURL: stwURL, Site: site, Token: tok}
		fedi = func(since, until time.Time) ([]stw.Item, error) {
			return client.Items(context.Background(), since, until)
		}
	}
	ed, err := buildEdition(session, fedi, time.Now(), time.Local, os.Stderr)
	session.Close()
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	url, wait, err := web.Serve(ctx, web.Handler(ed, time.Local))
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
