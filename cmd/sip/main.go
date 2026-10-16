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
	"nmail/internal/sip/web"
)

func main() {
	var c mailbox.Credentials
	fs := flag.NewFlagSet("sip", flag.ExitOnError)
	fs.StringVar(&c.Host, "host", envOr("SIP_HOST", ""), "IMAP server address (env SIP_HOST)")
	fs.StringVar(&c.Port, "port", envOr("SIP_PORT", "993"), "IMAP TLS port (env SIP_PORT)")
	fs.StringVar(&c.Username, "user", envOr("SIP_USER", ""), "login username (env SIP_USER)")
	fs.Usage = func() {
		fmt.Fprintln(os.Stderr, "usage: sip [-host h] [-port p] [-user u] [login]")
		fs.PrintDefaults()
	}
	fs.Parse(os.Args[1:])

	if fs.NArg() > 1 || (fs.NArg() == 1 && fs.Arg(0) != "login") {
		fs.Usage()
		os.Exit(2)
	}
	if c.Host == "" || c.Username == "" {
		fs.Usage()
		os.Exit(2)
	}

	store := account.Keyring{Service: "sip"}
	var err error
	if fs.Arg(0) == "login" {
		err = login(store, c, promptPassword(c))
	} else {
		err = run(store, c)
	}
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(store account.Keyring, c mailbox.Credentials) error {
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
	ed, err := buildEdition(session, time.Now(), time.Local, os.Stderr)
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
