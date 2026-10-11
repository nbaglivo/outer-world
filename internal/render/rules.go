package render

import (
	"regexp"
	"strings"

	"nmail/internal/mailbox"
)

// A Rule renders the messages whose subject matches it its own way.
type Rule struct {
	Name    string
	Subject *regexp.Regexp
	Render  func(*mailbox.Message) Doc
}

// Rules are tried in order; the first match wins, Default otherwise.
var Rules = []Rule{
	{Name: "daily digest", Subject: regexp.MustCompile(`(?i)\bdaily digest\b`), Render: Digest},
}

// Message renders msg with the first matching rule, or Default. It also
// returns the rule's name, "" for Default.
func Message(msg *mailbox.Message) (Doc, string) {
	for _, r := range Rules {
		if r.Subject.MatchString(msg.Subject) {
			return r.Render(msg), r.Name
		}
	}
	return Default(msg), ""
}

// Default prefers the plain-text part, as senders intend it for terminals.
func Default(msg *mailbox.Message) Doc {
	switch {
	case msg.Text != "":
		return Text(msg.Text)
	case msg.HTML != "":
		return HTML(msg.HTML)
	default:
		return Text(msg.Body)
	}
}

// Digest renders the Daily Digest from its HTML part. Its per-pick 👍/👎
// feedback links are drawn as buttons, like the rating ones at the end.
func Digest(msg *mailbox.Message) Doc {
	if msg.HTML == "" {
		return Default(msg)
	}
	return HTMLWith(msg.HTML, Options{Chip: func(l Link) bool {
		return strings.Contains(l.URL, "/digest-feedback")
	}})
}
