package main

import (
	"flag"
	"fmt"
	"os"

	tea "github.com/charmbracelet/bubbletea"

	"nmail/internal/account"
	"nmail/internal/mailbox"
	"nmail/internal/ui"
)

func main() {
	var defaults mailbox.Credentials
	flag.StringVar(&defaults.Host, "host", envOr("NMAIL_HOST", ""), "IMAP server address (env NMAIL_HOST)")
	flag.StringVar(&defaults.Port, "port", envOr("NMAIL_PORT", "993"), "IMAP TLS port (env NMAIL_PORT)")
	flag.StringVar(&defaults.Username, "user", envOr("NMAIL_USER", ""), "login username (env NMAIL_USER)")
	flag.Parse()

	app := ui.New(defaults, account.Keyring{})
	defer app.Close()

	if _, err := tea.NewProgram(app, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
