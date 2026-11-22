package mastodon

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type Account struct {
	Acct        string `json:"acct"`
	DisplayName string `json:"display_name"`
	URL         string `json:"url"`
}

type Attachment struct {
	Type        string `json:"type"`
	URL         string `json:"url"`
	Description string `json:"description"`
}

type Card struct {
	Title string `json:"title"`
	URL   string `json:"url"`
}

type Status struct {
	ID          string       `json:"id"`
	URI         string       `json:"uri"`
	URL         string       `json:"url"`
	CreatedAt   time.Time    `json:"created_at"`
	Account     Account      `json:"account"`
	Content     string       `json:"content"`
	SpoilerText string       `json:"spoiler_text"`
	Sensitive   bool         `json:"sensitive"`
	Reblog      *Status      `json:"reblog"`
	Attachments []Attachment `json:"media_attachments"`
	Card        *Card        `json:"card"`
}

type Timeline struct {
	Statuses []Status
	Complete bool
	Oldest   time.Time
}

type Client struct {
	BaseURL, Token string
	HTTP           *http.Client
	Timeout        time.Duration
}

const MaxPages = 25

const pageSize = "40"

func (c Client) Home(ctx context.Context, since time.Time) (Timeline, error) {
	timeout := c.Timeout
	if timeout == 0 {
		timeout = 15 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	base := strings.TrimRight(c.BaseURL, "/") + "/api/v1/timelines/home"
	var all []Status
	for range MaxPages {
		q := url.Values{"limit": {pageSize}}
		if len(all) > 0 {
			q.Set("max_id", all[len(all)-1].ID)
		}
		page, err := c.page(ctx, base+"?"+q.Encode())
		if err != nil {
			return Timeline{}, err
		}
		if len(page) == 0 {
			var oldest time.Time
			if len(all) > 0 {
				oldest = all[len(all)-1].CreatedAt
				if oldest.Before(since) {
					return Timeline{Statuses: all, Complete: true}, nil
				}
			}
			return Timeline{Statuses: all, Oldest: oldest}, nil
		}
		all = append(all, page...)
		older := true
		for _, s := range page {
			if !s.CreatedAt.Before(since) {
				older = false
				break
			}
		}
		if older {
			return Timeline{Statuses: all, Complete: true}, nil
		}
	}
	return Timeline{}, fmt.Errorf("GET home: more than %d pages", MaxPages)
}

func (c Client) page(ctx context.Context, u string) ([]Status, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return nil, fmt.Errorf("GET home: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+c.Token)
	req.Header.Set("Accept", "application/json")
	client := c.HTTP
	if client == nil {
		client = http.DefaultClient
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("GET home: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusPartialContent {
		return nil, fmt.Errorf("GET home: %d: home feed is regenerating", resp.StatusCode)
	}
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		var e struct {
			Error string `json:"error"`
		}
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<16))
		if json.Unmarshal(body, &e) == nil && e.Error != "" {
			return nil, fmt.Errorf("GET home: %d: %s", resp.StatusCode, e.Error)
		}
		return nil, fmt.Errorf("GET home: %d", resp.StatusCode)
	}
	var page []Status
	if err := json.NewDecoder(resp.Body).Decode(&page); err != nil {
		return nil, fmt.Errorf("GET home: decode: %w", err)
	}
	return page, nil
}
