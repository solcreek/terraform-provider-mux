// SPDX-License-Identifier: MPL-2.0

package mux

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func newTestClient(t *testing.T, h http.HandlerFunc) *Client {
	t.Helper()
	srv := httptest.NewServer(h)
	t.Cleanup(srv.Close)
	c := New("id", "secret", WithBaseURL(srv.URL+"/"), WithUserAgent("test-agent"))
	c.retryBase = time.Millisecond
	return c
}

func TestRequestShape(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		user, pass, ok := r.BasicAuth()
		if !ok || user != "id" || pass != "secret" {
			t.Errorf("basic auth = %q %q %v", user, pass, ok)
		}
		if r.Header.Get("User-Agent") != "test-agent" || r.Header.Get("Accept") != "application/json" {
			t.Errorf("headers = %v", r.Header)
		}
		if r.Method != http.MethodPost || r.URL.Path != "/system/v1/webhooks" || r.Header.Get("Content-Type") != "application/json" {
			t.Errorf("request = %s %s (%s)", r.Method, r.URL.Path, r.Header.Get("Content-Type"))
		}
		body, _ := io.ReadAll(r.Body)
		if string(body) != `{"address":"https://example.com/hook"}` {
			t.Errorf("body = %s", body)
		}
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"data":{"id":"abc12d","address":"https://example.com/hook","enabled":true,"signing_secret":"s3"}}`))
	})
	w, err := c.CreateWebhook(context.Background(), "https://example.com/hook")
	if err != nil {
		t.Fatal(err)
	}
	if w.ID != "abc12d" || !w.Enabled || w.SigningSecret != "s3" {
		t.Fatalf("webhook = %+v", w)
	}
}

// Payloads that depend on what is and is not sent.
func TestPayloads(t *testing.T) {
	var got []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		got = append(got, r.Method+" "+r.URL.EscapedPath()+" "+string(body))
		_, _ = w.Write([]byte(`{"data":{}}`))
	})
	ctx := context.Background()
	empty := ""
	steps := []func() error{
		// An empty domain list must be [] to deny every domain.
		func() error { _, err := c.UpdateReferrerRestriction(ctx, "r1", ReferrerRestriction{}); return err },
		// Unset fields stay out of a create, so Mux's defaults apply.
		func() error { _, err := c.CreateLiveStream(ctx, LiveStreamCreate{}); return err },
		// An empty string in an update clears the value; nil leaves it.
		func() error {
			_, err := c.UpdateLiveStream(ctx, "l1", LiveStreamUpdate{Passthrough: &empty})
			return err
		},
		func() error { _, err := c.SetGeneratedSubtitles(ctx, "l1", nil); return err },
		func() error { return c.SetStaticRenditions(ctx, "l1", []string{"highest"}) },
		func() error { return c.SetStaticRenditions(ctx, "l1", nil) },
		func() error { _, err := c.UpdateTranscriptionVocabulary(ctx, "v/1", "", nil, ""); return err },
		func() error { _, err := c.CreateSigningKey(ctx); return err },
	}
	for i, step := range steps {
		if err := step(); err != nil {
			t.Fatalf("step %d: %v", i, err)
		}
	}
	want := []string{
		`PUT /video/v1/playback-restrictions/r1/referrer {"allowed_domains":[],"allow_no_referrer":false}`,
		`POST /video/v1/live-streams {}`,
		`PATCH /video/v1/live-streams/l1 {"passthrough":""}`,
		`PUT /video/v1/live-streams/l1/generated-subtitles {"generated_subtitles":[]}`,
		`PUT /video/v1/live-streams/l1/new-asset-settings/static-renditions {"static_renditions":[{"resolution":"highest"}]}`,
		`DELETE /video/v1/live-streams/l1/new-asset-settings/static-renditions `,
		`PUT /video/v1/transcription-vocabularies/v%2F1 {"phrases":[]}`,
		`POST /system/v1/signing-keys `,
	}
	for i := range want {
		if i >= len(got) || got[i] != want[i] {
			t.Errorf("request %d = %q, want %q", i, got[min(i, len(got)-1)], want[i])
		}
	}
}

func TestListPaginates(t *testing.T) {
	var pages []string
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		pages = append(pages, q.Get("page")+"/"+q.Get("limit"))
		items := []map[string]string{}
		n := map[string]int{"1": 100, "2": 100, "3": 7}[q.Get("page")]
		for range n {
			items = append(items, map[string]string{"id": "x"})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"data": items})
	})
	got, err := c.ListPlaybackRestrictions(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 207 || strings.Join(pages, " ") != "1/100 2/100 3/100" {
		t.Fatalf("%d items over pages %v", len(got), pages)
	}
}

// A signing key is looked up in the list, which unlike a GET by ID never
// serves a deleted key.
func TestGetSigningKeyReadsTheList(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/system/v1/signing-keys" {
			t.Errorf("request = %s %s", r.Method, r.URL.Path)
		}
		_, _ = w.Write([]byte(`{"data":[{"id":"k1","created_at":"1"}]}`))
	})
	if key, err := c.GetSigningKey(context.Background(), "k1"); err != nil || key.ID != "k1" {
		t.Fatalf("key %+v, err %v", key, err)
	}
	if _, err := c.GetSigningKey(context.Background(), "gone"); !IsNotFound(err) {
		t.Fatalf("err = %v, want a 404", err)
	}
}

// Mux answers {"data": {}} for an empty DRM configuration list.
func TestListToleratesObject(t *testing.T) {
	c := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"data": {}}`))
	})
	got, err := c.ListDRMConfigurations(context.Background())
	if err != nil || len(got) != 0 {
		t.Fatalf("got %v, err %v", got, err)
	}
}

func TestRateLimitRetries(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) <= 3 {
			w.Header().Set("Retry-After", "0")
			w.WriteHeader(http.StatusTooManyRequests)
			_, _ = w.Write([]byte(`{"errors":["Too many requests, retry later."]}`))
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"k1","created_at":"1"}}`))
	})
	// A write is retried as well: a 429 is refused before Mux acts on it.
	key, err := c.CreateSigningKey(context.Background())
	if err != nil || key.ID != "k1" || calls.Load() != 4 {
		t.Fatalf("key %+v, err %v after %d calls", key, err, calls.Load())
	}
}

func TestRateLimitGivesUp(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		w.Header().Set("Retry-After", "0")
		w.WriteHeader(http.StatusTooManyRequests)
		_, _ = w.Write([]byte(`{"errors":["Too many requests, retry later."]}`))
	})
	c.maxRetries = 2
	_, err := c.WhoAmI(context.Background())
	if StatusCode(err) != http.StatusTooManyRequests || calls.Load() != 3 || !strings.Contains(err.Error(), "Too many requests") {
		t.Fatalf("err = %v after %d calls", err, calls.Load())
	}
}

func TestRetryWait(t *testing.T) {
	c := New("id", "secret")
	cases := []struct {
		name    string
		h       http.Header
		attempt int
		want    time.Duration
	}{
		{"retry-after", http.Header{"Retry-After": []string{"7"}}, 0, 7 * time.Second},
		{"retry-after is capped", http.Header{"Retry-After": []string{"3600"}}, 0, maxRetryWait},
		{"no hint, first attempt", http.Header{}, 0, time.Second},
		{"no hint, third attempt", http.Header{}, 2, 4 * time.Second},
	}
	for _, tc := range cases {
		if got := c.retryWait(tc.h, tc.attempt); got != tc.want {
			t.Errorf("%s: wait = %v, want %v", tc.name, got, tc.want)
		}
	}
}

// A GET that dies on the network is repeated; a write never is.
func TestNetworkRetries(t *testing.T) {
	var calls atomic.Int32
	c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			conn, _, _ := w.(http.Hijacker).Hijack()
			_ = conn.Close()
			return
		}
		_, _ = w.Write([]byte(`{"data":{"id":"k1"}}`))
	})
	if _, err := c.GetWebhook(context.Background(), "k1"); err != nil || calls.Load() != 2 {
		t.Fatalf("GET: err %v after %d calls", err, calls.Load())
	}
	calls.Store(0)
	if _, err := c.CreateSigningKey(context.Background()); err == nil || calls.Load() != 1 {
		t.Fatalf("POST: err %v after %d calls", err, calls.Load())
	}
}

func TestErrorMessages(t *testing.T) {
	cases := []struct {
		status int
		body   string
		want   string
	}{
		{401, `{"error":{"type":"unauthorized","messages":["Unauthorized request"]}}`, "Unauthorized request (the access token ID or secret is missing, wrong or revoked"},
		{403, `{"error":{"type":"forbidden","messages":["Forbidden"]}}`, "lacks a permission"},
		{404, `{"error":{"type":"not_found","messages":["Playback restriction not found"]}}`, "mux API error 404: Playback restriction not found"},
		{400, `{"error":{"type":"invalid_parameters","messages":["Malformed entry: a","Malformed entry: b"]}}`, "Malformed entry: a; Malformed entry: b"},
		{502, `<html>bad gateway</html>`, "mux API error 502: <html>bad gateway</html>"},
	}
	for _, tc := range cases {
		c := newTestClient(t, func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(tc.status)
			_, _ = w.Write([]byte(tc.body))
		})
		_, err := c.WhoAmI(context.Background())
		if err == nil || !strings.Contains(err.Error(), tc.want) {
			t.Errorf("%d: error = %v, want it to contain %q", tc.status, err, tc.want)
		}
		if StatusCode(err) != tc.status || IsNotFound(err) != (tc.status == 404) {
			t.Errorf("%d: StatusCode = %d, IsNotFound = %v", tc.status, StatusCode(err), IsNotFound(err))
		}
	}
	// The secret never shows up in an error.
	c := New("id", "hunter2", WithBaseURL("http://127.0.0.1:1"))
	c.retryBase = time.Millisecond
	if _, err := c.WhoAmI(context.Background()); err == nil || strings.Contains(err.Error(), "hunter2") {
		t.Errorf("network error = %v", err)
	}
}
