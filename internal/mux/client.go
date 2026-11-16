// SPDX-License-Identifier: MPL-2.0

// Package mux is a minimal, dependency-free (stdlib only) client for the
// subset of the Mux API (https://www.mux.com/docs/api-reference) that this
// Terraform provider needs: live streams with their playback IDs and
// simulcast targets, signing keys, webhooks, playback restrictions,
// transcription vocabularies and DRM configurations.
//
// It follows Mux's published API reference and OpenAPI specification
// (https://www.mux.com/api-spec.json) and was checked against api.mux.com
// (2026-11-16). Where the live API differs from the specification, the code
// follows the live API and says so. Mux's writes are synchronous.
package mux

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

// DefaultBaseURL is the Mux API.
const DefaultBaseURL = "https://api.mux.com"

// maxConcurrent caps requests in flight. Mux rate limits per second (about
// one write and five reads), and Terraform runs up to ten operations at once.
const maxConcurrent = 2

// maxNetworkRetries is how often a GET is repeated after a transport error
// (timeout, reset connection).
const maxNetworkRetries = 2

// maxRetryWait bounds how long one rate-limit back-off may sleep.
const maxRetryWait = 60 * time.Second

// pageSize is how many items a list request asks for. Transcription
// vocabularies allow at most 10 and pass their own limit.
const pageSize = 100

// Client is a Mux API client. It is safe for concurrent use.
type Client struct {
	http        *http.Client
	baseURL     string
	tokenID     string
	tokenSecret string
	userAgent   string
	maxRetries  int
	retryBase   time.Duration
	slots       chan struct{}
}

// Option configures a Client.
type Option func(*Client)

// WithBaseURL overrides the API base URL. A trailing slash is ignored.
func WithBaseURL(u string) Option {
	return func(c *Client) {
		if u != "" {
			c.baseURL = strings.TrimRight(u, "/")
		}
	}
}

// WithTimeout sets the per-request HTTP timeout.
func WithTimeout(d time.Duration) Option {
	return func(c *Client) {
		if d > 0 {
			c.http.Timeout = d
		}
	}
}

// WithHTTPClient injects a custom *http.Client (mainly for tests).
func WithHTTPClient(h *http.Client) Option {
	return func(c *Client) {
		if h != nil {
			c.http = h
		}
	}
}

// WithUserAgent sets the User-Agent header.
func WithUserAgent(ua string) Option {
	return func(c *Client) {
		if ua != "" {
			c.userAgent = ua
		}
	}
}

// New builds a Client authenticated with a Mux access token: its ID and
// secret, sent as HTTP basic credentials. A token belongs to one environment.
func New(tokenID, tokenSecret string, opts ...Option) *Client {
	c := &Client{
		http:        &http.Client{Timeout: 60 * time.Second},
		baseURL:     DefaultBaseURL,
		tokenID:     tokenID,
		tokenSecret: tokenSecret,
		userAgent:   "terraform-provider-mux",
		maxRetries:  5,
		retryBase:   time.Second,
		slots:       make(chan struct{}, maxConcurrent),
	}
	for _, o := range opts {
		o(c)
	}
	return c
}

// APIError is returned for any non-2xx response.
type APIError struct {
	StatusCode int
	// Type is Mux's machine-readable error type, e.g. "invalid_parameters".
	Type    string
	Message string
	Body    string
}

func (e *APIError) Error() string {
	if e.Message != "" {
		return fmt.Sprintf("mux API error %d: %s", e.StatusCode, e.Message)
	}
	return fmt.Sprintf("mux API error %d: %s", e.StatusCode, strings.TrimSpace(e.Body))
}

// IsNotFound reports whether err is an API 404. Mux answers 404 for an
// object that was deleted, and 400 ("Failed to parse ID") for an ID that
// could never have existed, which is not treated as absence.
func IsNotFound(err error) bool {
	var ae *APIError
	return errors.As(err, &ae) && ae.StatusCode == http.StatusNotFound
}

// StatusCode returns the HTTP status of an APIError, or 0.
func StatusCode(err error) int {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.StatusCode
	}
	return 0
}

// rawResponse is one HTTP exchange, before the status is interpreted.
type rawResponse struct {
	status int
	body   []byte
}

// send performs a request with retries on rate limiting and returns the raw
// response whatever its status.
func (c *Client) send(ctx context.Context, method, path string, query url.Values, body any) (*rawResponse, error) {
	var payload []byte
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			return nil, fmt.Errorf("marshal request body: %w", err)
		}
		payload = b
	}

	u := c.baseURL + path
	if len(query) > 0 {
		u += "?" + query.Encode()
	}

	for attempt := 0; ; attempt++ {
		var rdr io.Reader
		if payload != nil {
			rdr = bytes.NewReader(payload)
		}
		req, err := http.NewRequestWithContext(ctx, method, u, rdr)
		if err != nil {
			return nil, err
		}
		req.SetBasicAuth(c.tokenID, c.tokenSecret)
		req.Header.Set("Accept", "application/json")
		req.Header.Set("User-Agent", c.userAgent)
		if payload != nil {
			req.Header.Set("Content-Type", "application/json")
		}

		// The slot is held only for the HTTP round trip, never while a
		// back-off sleeps.
		select {
		case c.slots <- struct{}{}:
		case <-ctx.Done():
			return nil, ctx.Err()
		}
		resp, err := c.http.Do(req)
		var respBody []byte
		if err == nil {
			respBody, err = io.ReadAll(resp.Body)
			_ = resp.Body.Close()
		}
		<-c.slots
		if err != nil {
			// A read that died on the network is safe to repeat; a write is
			// not, since it may have landed.
			if method == http.MethodGet && ctx.Err() == nil && attempt < maxNetworkRetries {
				select {
				case <-ctx.Done():
					return nil, ctx.Err()
				case <-time.After(time.Duration(1<<attempt) * c.retryBase):
				}
				continue
			}
			return nil, fmt.Errorf("%s %s: %w", method, path, err)
		}

		// A 429 is refused before Mux acts on the request, so repeating it
		// is safe for writes too.
		if resp.StatusCode == http.StatusTooManyRequests && attempt < c.maxRetries {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(c.retryWait(resp.Header, attempt)):
			}
			continue
		}
		return &rawResponse{status: resp.StatusCode, body: respBody}, nil
	}
}

// retryWait is how long to back off after a 429: Retry-After when present,
// else exponentially: 1s, 2s, 4s, 8s, 16s.
func (c *Client) retryWait(h http.Header, attempt int) time.Duration {
	wait := time.Duration(1<<attempt) * c.retryBase
	if secs, err := strconv.Atoi(strings.TrimSpace(h.Get("Retry-After"))); err == nil && secs >= 0 {
		wait = time.Duration(secs) * time.Second
	}
	if wait > maxRetryWait {
		wait = maxRetryWait
	}
	return wait
}

// envelope is the {"data": ...} wrapper around every Mux response.
type envelope[T any] struct {
	Data T `json:"data"`
}

// do performs a request and decodes the "data" of a 2xx response into out
// (if non-nil).
func (c *Client) do(ctx context.Context, method, path string, query url.Values, body, out any) error {
	raw, err := c.send(ctx, method, path, query, body)
	if err != nil {
		return err
	}
	if raw.status < 200 || raw.status > 299 {
		return parseError(raw.status, raw.body)
	}
	if out != nil && len(raw.body) > 0 {
		if err := json.Unmarshal(raw.body, &envelope[any]{Data: out}); err != nil {
			return fmt.Errorf("decode %s %s response: %w", method, path, err)
		}
	}
	return nil
}

// list fetches every page of a list endpoint. Mux paginates with `page` and
// `limit`; a page shorter than the limit is the last one, and a page past
// the end is empty.
func list[T any](ctx context.Context, c *Client, path string, limit int) ([]T, error) {
	var all []T
	for page := 1; ; page++ {
		q := url.Values{"limit": {strconv.Itoa(limit)}, "page": {strconv.Itoa(page)}}
		var raw json.RawMessage
		if err := c.do(ctx, http.MethodGet, path, q, nil, &raw); err != nil {
			return nil, err
		}
		var items []T
		// An empty DRM configuration list arrives as {"data": {}} rather
		// than an empty array (observed 2026-11-16).
		if trimmed := bytes.TrimSpace(raw); len(trimmed) > 0 && trimmed[0] == '[' {
			if err := json.Unmarshal(trimmed, &items); err != nil {
				return nil, fmt.Errorf("decode GET %s response: %w", path, err)
			}
		}
		all = append(all, items...)
		if len(items) < limit {
			return all, nil
		}
	}
}

func parseError(status int, body []byte) error {
	ae := &APIError{StatusCode: status, Body: string(body)}
	ae.Type, ae.Message = errorMessage(body)
	// Make credential problems actionable.
	switch status {
	case http.StatusUnauthorized:
		ae.Message = strings.TrimSpace(ae.Message + " (the access token ID or secret is missing, wrong or revoked — check MUX_TOKEN_ID and MUX_TOKEN_SECRET)")
	case http.StatusForbidden:
		ae.Message = strings.TrimSpace(ae.Message + " (the access token lacks a permission this call needs, or the feature is not enabled for the environment)")
	}
	return ae
}

// errorMessage extracts the type and a readable reason from a Mux error
// body: {"error": {"type": "...", "messages": ["..."]}}, or, when rate
// limited, {"errors": ["..."]}.
func errorMessage(body []byte) (typ, msg string) {
	var e struct {
		Error struct {
			Type     string   `json:"type"`
			Messages []string `json:"messages"`
		} `json:"error"`
		Errors []string `json:"errors"`
	}
	if json.Unmarshal(body, &e) != nil {
		return "", ""
	}
	msgs := append(e.Error.Messages, e.Errors...)
	return e.Error.Type, strings.Join(msgs, "; ")
}

// seg escapes one path segment.
func seg(s string) string { return url.PathEscape(s) }
