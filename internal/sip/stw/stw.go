// Package stw reads the edition window of items from the stw reader API.
package stw

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type Account struct {
	ID         string `json:"id"`
	Handle     string `json:"handle"`
	Name       string `json:"name"`
	ProfileURL string `json:"profile_url"`
}

type Boost struct {
	By Account `json:"by"`
}

type Attachment struct {
	URL         string `json:"url"`
	MediaType   string `json:"media_type"`
	Kind        string `json:"kind"`
	Description string `json:"description"`
}

type Item struct {
	ID          string       `json:"id"`
	URL         string       `json:"url"`
	Type        string       `json:"type"`
	Published   time.Time    `json:"published"`
	Title       string       `json:"title"`
	Summary     string       `json:"summary"`
	Sensitive   bool         `json:"sensitive"`
	ContentHTML string       `json:"content_html"`
	ContentText string       `json:"content_text"`
	Author      Account      `json:"author"`
	Boost       *Boost       `json:"boost"`
	Attachments []Attachment `json:"attachments"`
	ReceivedAt  time.Time    `json:"received_at"`
}

type Client struct {
	BaseURL, Site, Token string
	HTTP                 *http.Client  // nil: http.DefaultClient
	Timeout              time.Duration // 0: 15 * time.Second
}

const (
	MaxPages       = 25
	defaultTimeout = 15 * time.Second
	pageLimit      = 200
	maxBody        = 16 << 20
	timeLayout     = "2006-01-02T15:04:05.000Z"
)

type listResponse struct {
	Items      []Item  `json:"items"`
	NextCursor *string `json:"next_cursor"`
}

// Items fetches every item published in [since, until), oldest first. Any
// error discards the pages already read.
func (c Client) Items(ctx context.Context, since, until time.Time) ([]Item, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	endpoint := strings.TrimRight(c.BaseURL, "/") + "/reader/v1/sites/" + url.PathEscape(c.Site) + "/items"
	var all []Item
	cursor := ""
	for pages := 0; ; pages++ {
		if pages == MaxPages {
			return nil, fmt.Errorf("GET items: more than %d pages", MaxPages)
		}
		p, err := c.page(ctx, endpoint, since, until, cursor)
		if err != nil {
			return nil, err
		}
		all = append(all, p.Items...)
		if p.NextCursor == nil || len(p.Items) == 0 {
			return all, nil
		}
		cursor = *p.NextCursor
	}
}

func (c Client) page(ctx context.Context, endpoint string, since, until time.Time, cursor string) (*listResponse, error) {
	q := url.Values{}
	q.Set("since", since.UTC().Format(timeLayout))
	q.Set("until", until.UTC().Format(timeLayout))
	q.Set("order", "asc")
	q.Set("limit", strconv.Itoa(pageLimit))
	if cursor != "" {
		q.Set("cursor", cursor)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"?"+q.Encode(), nil)
	if err != nil {
		return nil, fmt.Errorf("GET items: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")

	hc := c.HTTP
	if hc == nil {
		hc = http.DefaultClient
	}
	resp, err := hc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET items: %w", err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxBody))
	if err != nil {
		return nil, fmt.Errorf("GET items: %w", err)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
			Code  string `json:"code"`
		}
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("GET items: %s: %s (%s)", resp.Status, e.Error, e.Code)
		}
		return nil, fmt.Errorf("GET items: %s", resp.Status)
	}
	var p listResponse
	if err := json.Unmarshal(body, &p); err != nil {
		return nil, fmt.Errorf("GET items: decode response: %w", err)
	}
	return &p, nil
}
