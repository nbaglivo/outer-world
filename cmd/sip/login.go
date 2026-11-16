package main

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/term"

	"nmail/internal/mailbox"
)

// login asks for the password and saves c as the one saved login.
func login(store interface{ Save([]mailbox.Credentials) error }, c mailbox.Credentials, readPassword func() (string, error)) error {
	pw, err := readPassword()
	if err != nil {
		return err
	}
	if pw == "" {
		return errors.New("empty password")
	}
	c.Password = pw
	return store.Save([]mailbox.Credentials{c})
}

// promptPassword reads a password from the terminal without echoing it.
func promptPassword(c mailbox.Credentials) func() (string, error) {
	return func() (string, error) {
		fmt.Fprintf(os.Stderr, "Password for %s@%s: ", c.Username, c.Host)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
}

// loginToken asks for the stw reader token and saves it.
func loginToken(store interface{ Set(string) error }, readToken func() (string, error)) error {
	tok, err := readToken()
	if err != nil {
		return err
	}
	tok = strings.TrimSpace(tok)
	if tok == "" {
		return errors.New("empty token")
	}
	return store.Set(tok)
}

// promptToken reads a reader token from the terminal without echoing it.
func promptToken(site, base string) func() (string, error) {
	return func() (string, error) {
		fmt.Fprintf(os.Stderr, "Reader token for %s at %s: ", site, base)
		b, err := term.ReadPassword(int(os.Stdin.Fd()))
		fmt.Fprintln(os.Stderr)
		return string(b), err
	}
}
