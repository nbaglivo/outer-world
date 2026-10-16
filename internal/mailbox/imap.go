// Package mailbox talks to an IMAP server: login, list INBOX, read and delete
// messages.
package mailbox

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"net/textproto"
	"regexp"
	"slices"
	"sort"
	"strings"
	"time"

	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	_ "github.com/emersion/go-message/charset"
	"github.com/emersion/go-message/mail"
)

type Credentials struct {
	Host     string
	Port     string
	Username string
	Password string
}

type Summary struct {
	UID     imap.UID
	From    string
	Subject string
	Date    time.Time
	Seen    bool
}

type Message struct {
	Summary
	To   string
	Body string // Text, or HTML crudely de-tagged when there is no Text
	Text string // the text/plain part, if any
	HTML string // the text/html part, if any
	Raw  []byte // the whole RFC 822 message, for handing it to other programs

	Arrived   time.Time // the IMAP internal date
	FromAddr  string    // first From address, user@domain
	MessageID string    // envelope Message-ID, without <>
	ListID    string    // raw List-Id header value
	ListPost  string    // raw List-Post header value
}

type Session struct {
	client *imapclient.Client
}

func Connect(c Credentials) (*Session, error) {
	client, err := imapclient.DialTLS(c.Host+":"+c.Port, nil)
	if err != nil {
		return nil, fmt.Errorf("connect: %w", err)
	}
	if err := client.Login(c.Username, c.Password).Wait(); err != nil {
		client.Close()
		return nil, fmt.Errorf("login: %w", err)
	}
	return &Session{client: client}, nil
}

func (s *Session) Close() {
	s.client.Logout().Wait()
	s.client.Close()
}

// Inbox returns the newest `limit` messages of INBOX, newest first.
func (s *Session) Inbox(limit uint32) ([]Summary, error) {
	sel, err := s.client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}
	if sel.NumMessages == 0 {
		return nil, nil
	}

	first := uint32(1)
	if sel.NumMessages > limit {
		first = sel.NumMessages - limit + 1
	}
	var set imap.SeqSet
	set.AddRange(first, sel.NumMessages)

	bufs, err := s.client.Fetch(set, &imap.FetchOptions{
		UID:      true,
		Envelope: true,
		Flags:    true,
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	out := make([]Summary, 0, len(bufs))
	for _, b := range bufs {
		out = append(out, summarize(b))
	}
	sort.Slice(out, func(i, j int) bool { return out[i].UID > out[j].UID })
	return out, nil
}

// Read downloads the full message with the given UID without marking it seen.
func (s *Session) Read(uid imap.UID) (*Message, error) {
	section := &imap.FetchItemBodySection{Peek: true}
	bufs, err := s.client.Fetch(imap.UIDSetNum(uid), &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}
	if len(bufs) == 0 {
		return nil, fmt.Errorf("message %d not found", uid)
	}
	return message(bufs[0], section)
}

// Window returns the INBOX messages that arrived at or after from and before
// to, oldest first. It never marks anything seen.
func (s *Session) Window(from, to time.Time) ([]*Message, error) {
	if _, err := s.client.Select("INBOX", &imap.SelectOptions{ReadOnly: true}).Wait(); err != nil {
		return nil, fmt.Errorf("select INBOX: %w", err)
	}
	found, err := s.client.UIDSearch(&imap.SearchCriteria{Since: from}, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("search: %w", err)
	}
	uids := found.AllUIDs()
	if len(uids) == 0 {
		return nil, nil
	}

	section := &imap.FetchItemBodySection{Peek: true}
	bufs, err := s.client.Fetch(imap.UIDSetNum(uids...), &imap.FetchOptions{
		UID:          true,
		Envelope:     true,
		Flags:        true,
		InternalDate: true,
		BodySection:  []*imap.FetchItemBodySection{section},
	}).Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	var out []*Message
	for _, b := range bufs {
		if b.InternalDate.Before(from) || !b.InternalDate.Before(to) {
			continue
		}
		m, err := message(b, section)
		if err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].Arrived.Before(out[j].Arrived) })
	return out, nil
}

// message builds a Message from a fetched buffer that includes section.
func message(b *imapclient.FetchMessageBuffer, section *imap.FetchItemBodySection) (*Message, error) {
	msg := &Message{Summary: summarize(b), Arrived: b.InternalDate}
	if env := b.Envelope; env != nil {
		msg.To = formatAddrs(env.To)
		msg.MessageID = strings.Trim(env.MessageID, "<>")
		if len(env.From) > 0 {
			msg.FromAddr = env.From[0].Addr()
		}
	}
	msg.Raw = b.FindBodySection(section)
	msg.ListID, msg.ListPost = listHeaders(msg.Raw)
	var err error
	msg.Text, msg.HTML, err = extractParts(msg.Raw)
	if err != nil {
		return nil, err
	}
	msg.Body = bodyText(msg.Text, msg.HTML)
	return msg, nil
}

// Parse builds a Message from a raw RFC 822 message. Arrived and UID are left
// zero for the caller.
func Parse(raw []byte) (*Message, error) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return nil, fmt.Errorf("parse message: %w", err)
	}
	h := r.Header
	msg := &Message{Raw: raw}
	msg.Subject, _ = h.Subject()
	msg.Date, _ = h.Date()
	if from, _ := h.AddressList("From"); len(from) > 0 {
		msg.From = joinNames(from)
		msg.FromAddr = from[0].Address
	}
	if to, _ := h.AddressList("To"); len(to) > 0 {
		msg.To = joinNames(to)
	}
	msg.MessageID, _ = h.MessageID()
	msg.ListID, msg.ListPost = listHeaders(raw)
	msg.Text, msg.HTML, err = extractParts(raw)
	if err != nil {
		return nil, err
	}
	msg.Body = bodyText(msg.Text, msg.HTML)
	return msg, nil
}

// joinNames is formatAddrs for parsed header addresses.
func joinNames(addrs []*mail.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, a.Name)
		} else {
			parts = append(parts, a.Address)
		}
	}
	return strings.Join(parts, ", ")
}

// listHeaders returns the raw List-Id and List-Post header values of a raw
// RFC 822 message, or "" for each that is missing or unparsable.
func listHeaders(raw []byte) (listID, listPost string) {
	h, err := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw))).ReadMIMEHeader()
	if h == nil && err != nil {
		return "", ""
	}
	return strings.TrimSpace(h.Get("List-Id")), strings.TrimSpace(h.Get("List-Post"))
}

// Delete removes the INBOX message with the given UID. It is moved to the
// server's Trash mailbox when there is one and expunged otherwise; trashed
// reports which.
func (s *Session) Delete(uid imap.UID) (trashed bool, err error) {
	if _, err := s.client.Select("INBOX", nil).Wait(); err != nil {
		return false, fmt.Errorf("select INBOX: %w", err)
	}
	set := imap.UIDSetNum(uid)

	trash, err := s.trashMailbox()
	if err != nil {
		return false, err
	}
	if trash != "" {
		if _, err := s.client.Move(set, trash).Wait(); err != nil {
			return false, fmt.Errorf("move to %s: %w", trash, err)
		}
		return true, nil
	}

	store := &imap.StoreFlags{Op: imap.StoreFlagsAdd, Silent: true, Flags: []imap.Flag{imap.FlagDeleted}}
	if err := s.client.Store(set, store, nil).Close(); err != nil {
		return false, fmt.Errorf("flag deleted: %w", err)
	}
	expunge := s.client.Expunge()
	if s.client.Caps().Has(imap.CapUIDPlus) {
		expunge = s.client.UIDExpunge(set)
	}
	if err := expunge.Close(); err != nil {
		return false, fmt.Errorf("expunge: %w", err)
	}
	return false, nil
}

// trashMailbox finds the mailbox marked \Trash, falling back to one named
// "Trash". It returns "" when there is none.
func (s *Session) trashMailbox() (string, error) {
	caps := s.client.Caps()
	opts := &imap.ListOptions{ReturnSpecialUse: caps.Has(imap.CapSpecialUse) && caps.Has(imap.CapListExtended)}
	list, err := s.client.List("", "*", opts).Collect()
	if err != nil {
		return "", fmt.Errorf("list mailboxes: %w", err)
	}
	return pickTrash(list), nil
}

func pickTrash(list []*imap.ListData) string {
	var named string
	for _, m := range list {
		if slices.Contains(m.Attrs, imap.MailboxAttrTrash) {
			return m.Mailbox
		}
		if strings.EqualFold(m.Mailbox, "Trash") {
			named = m.Mailbox
		}
	}
	return named
}

func summarize(b *imapclient.FetchMessageBuffer) Summary {
	s := Summary{UID: b.UID, Date: b.InternalDate}
	for _, f := range b.Flags {
		if f == imap.FlagSeen {
			s.Seen = true
		}
	}
	if env := b.Envelope; env != nil {
		s.Subject = env.Subject
		s.From = formatAddrs(env.From)
		if !env.Date.IsZero() {
			s.Date = env.Date
		}
	}
	return s
}

func formatAddrs(addrs []imap.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, a.Name)
		} else {
			parts = append(parts, a.Addr())
		}
	}
	return strings.Join(parts, ", ")
}

// bodyText is what to show for a message: its text/plain part, falling back
// to a crudely de-tagged text/html part.
func bodyText(plain, html string) string {
	switch {
	case plain != "":
		return plain
	case html != "":
		return stripHTML(html)
	default:
		return "(no text content)"
	}
}

// extractParts returns the first text/plain and text/html parts of a raw
// RFC 822 message.
func extractParts(raw []byte) (plain, html string, err error) {
	r, err := mail.CreateReader(strings.NewReader(string(raw)))
	if err != nil {
		return "", "", fmt.Errorf("parse message: %w", err)
	}

	for {
		p, err := r.NextPart()
		if err == io.EOF {
			break
		}
		if err != nil {
			return "", "", fmt.Errorf("parse part: %w", err)
		}
		h, ok := p.Header.(*mail.InlineHeader)
		if !ok {
			continue
		}
		ct, _, _ := h.ContentType()
		body, _ := io.ReadAll(p.Body)
		switch {
		case ct == "text/plain" && plain == "":
			plain = string(body)
		case ct == "text/html" && html == "":
			html = string(body)
		}
	}
	return plain, html, nil
}

var (
	reDrop   = regexp.MustCompile(`(?is)<(style|script|head)[^>]*>.*?</(style|script|head)>`)
	reBreak  = regexp.MustCompile(`(?i)<(br|/p|/div|/tr|/h[1-6]|/li)[^>]*>`)
	reTag    = regexp.MustCompile(`<[^>]+>`)
	reBlanks = regexp.MustCompile(`\n{3,}`)
	entities = strings.NewReplacer("&nbsp;", " ", "&amp;", "&", "&lt;", "<", "&gt;", ">", "&quot;", `"`, "&#39;", "'")
)

func stripHTML(s string) string {
	s = reDrop.ReplaceAllString(s, "")
	s = reBreak.ReplaceAllString(s, "\n")
	s = reTag.ReplaceAllString(s, "")
	s = entities.Replace(s)
	return strings.TrimSpace(reBlanks.ReplaceAllString(s, "\n\n"))
}
