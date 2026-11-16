// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strconv"
	"strings"
	"sync"
	"testing"
)

const (
	fakeTokenID     = "00000000-0000-4000-8000-000000000001"
	fakeTokenSecret = "test-secret"
)

// fakeMux is an in-memory stand-in for the parts of the Mux API the provider
// uses, so every resource's Terraform lifecycle can be tested without
// credentials. It mirrors behaviour observed on api.mux.com (verified
// 2026-11-16 with direct API calls and TestAccAPIFacts):
//
//   - every response wraps its payload in {"data": ...}; errors are
//     {"error": {"type": "...", "messages": ["..."]}}; an object that was
//     deleted answers 404, and deleting it again is a 404 too;
//   - unset and false fields are left out of a response: a live stream
//     created with no arguments has no passthrough, meta, new_asset_settings,
//     subtitles or use_slate_for_standard_latency, and a playback
//     restriction's referrer is {} when no domain is allowed;
//   - a live stream defaults to latency "standard", reconnect window 60 and
//     max continuous duration 43200; a test stream is fixed at 300, ignores
//     another value on create and refuses any on update, and refuses
//     simulcast targets;
//   - PATCH on a live stream ignores audio_only and test, clears
//     passthrough, reconnect_slate_url and meta.title given "", and refuses
//     every new_asset_settings field but video_quality, which must be "plus"
//     or "premium";
//   - subtitles are refused on a low latency stream, low latency on a stream
//     with subtitles, and more than one generated track; their language
//     defaults to "en" and the embedded channel to "cc1";
//   - static renditions are replaced by a PUT that refuses an empty list,
//     and removed by a DELETE;
//   - a playback restriction keeps its domains in the order given, takes at
//     most 100, and its user agent rules default to true;
//   - a webhook address is stored unvalidated but must be unique (400), an
//     update needs at least one field, and the signing secret, like a signing
//     key's private key, is only in the create response;
//   - a transcription vocabulary update replaces everything: a name or
//     passthrough left out is removed;
//   - a signing key that was deleted keeps answering a GET by its ID with
//     200 for minutes, while the list of keys drops it at once;
//   - list endpoints page with `limit` and `page`, a page past the end is
//     empty, and an empty DRM configuration list is {"data": {}}.
type fakeMux struct {
	t   *testing.T
	srv *httptest.Server

	mu            sync.Mutex
	next          int
	streams       []*fakeStream
	restrictions  []*fakeRestriction
	keys          []*fakeSigningKey
	deletedKeys   []string
	webhooks      []*fakeWebhook
	vocabularies  []*fakeVocabulary
	drmConfigs    []string
	requests      []string
	rateLimitNext int
}

type fakeStream struct {
	ID, StreamKey, Passthrough, Title, LatencyMode, SlateURL string
	ReconnectWindow                                          float64
	MaxContinuousDuration                                    int64
	UseSlate, AudioOnly, Test                                bool
	AssetPolicies, StaticRenditions                          []string
	VideoQuality                                             string
	Embedded, Generated                                      []map[string]any
	PlaybackIDs                                              []*fakePlaybackID
	Simulcasts                                               []*fakeSimulcast
	// Active models a broadcast in progress.
	Active bool
}

type fakePlaybackID struct{ ID, Policy, DRMConfigurationID string }

type fakeSimulcast struct{ ID, URL, StreamKey, Passthrough string }

type fakeRestriction struct {
	ID                                        string
	Domains                                   []string
	AllowNoReferrer, AllowNoUA, AllowHighRisk bool
}

type fakeSigningKey struct{ ID string }

type fakeWebhook struct {
	ID, Address string
	Enabled     bool
}

type fakeVocabulary struct {
	ID, Name, Passthrough string
	Phrases               []string
}

func newFakeMux(t *testing.T) *fakeMux {
	t.Helper()
	for _, k := range []string{"MUX_TOKEN_ID", "MUX_TOKEN_SECRET", "MUX_BASE_URL"} {
		t.Setenv(k, "")
	}
	f := &fakeMux{t: t}
	f.srv = httptest.NewServer(f.routes())
	t.Cleanup(f.srv.Close)
	return f
}

func (f *fakeMux) providerConfig() string {
	return requiredProviders + fmt.Sprintf(`
provider "mux" {
  token_id     = %q
  token_secret = %q
  base_url     = %q
}
`, fakeTokenID, fakeTokenSecret, f.srv.URL)
}

func (f *fakeMux) id(prefix string) string {
	f.next++
	return fmt.Sprintf("%s%04dFakeMuxObjectId", prefix, f.next)
}

// ------------------------------------------------------------ test helpers

// mutate changes the fake's state out of band, as a person in the dashboard
// would.
func (f *fakeMux) mutate(fn func()) {
	f.mu.Lock()
	defer f.mu.Unlock()
	fn()
}

// addRestriction inserts a playback restriction, as if it had been made
// before Terraform came along.
func (f *fakeMux) addRestriction(domains ...string) *fakeRestriction {
	f.mu.Lock()
	defer f.mu.Unlock()
	r := &fakeRestriction{ID: f.id("pr"), Domains: domains, AllowNoUA: true, AllowHighRisk: true}
	f.restrictions = append(f.restrictions, r)
	return r
}

func find[T any](items []*T, id func(*T) string, want string) *T {
	for _, it := range items {
		if id(it) == want {
			return it
		}
	}
	return nil
}

func (f *fakeMux) stream(id string) *fakeStream {
	return find(f.streams, func(s *fakeStream) string { return s.ID }, id)
}

func (f *fakeMux) restriction(id string) *fakeRestriction {
	return find(f.restrictions, func(r *fakeRestriction) string { return r.ID }, id)
}

func (f *fakeMux) webhook(id string) *fakeWebhook {
	return find(f.webhooks, func(w *fakeWebhook) string { return w.ID }, id)
}

func (f *fakeMux) vocabulary(id string) *fakeVocabulary {
	return find(f.vocabularies, func(v *fakeVocabulary) string { return v.ID }, id)
}

// snapshot returns a copy of an object for assertions, or nil.
func snapshot[T any](f *fakeMux, get func(string) *T, id string) *T {
	f.mu.Lock()
	defer f.mu.Unlock()
	v := get(id)
	if v == nil {
		return nil
	}
	cp := *v
	return &cp
}

// sawRequest reports whether a request with this method and path prefix was
// served.
func (f *fakeMux) sawRequest(method, pathPrefix string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.ContainsFunc(f.requests, func(r string) bool { return strings.HasPrefix(r, method+" "+pathPrefix) })
}

func (f *fakeMux) forgetRequests() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.requests = nil
}

// ---------------------------------------------------------------- plumbing

func writeData(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"data": data})
}

func writeError(w http.ResponseWriter, status int, typ, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"error": map[string]any{"type": typ, "messages": []string{msg}}})
}

func invalid(w http.ResponseWriter, msg string) {
	writeError(w, http.StatusBadRequest, "invalid_parameters", msg)
}

func notFound(w http.ResponseWriter, what string) {
	writeError(w, http.StatusNotFound, "not_found", what+" not found")
}

func strs(v any) []string {
	raw, _ := v.([]any)
	out := make([]string, 0, len(raw))
	for _, x := range raw {
		if s, ok := x.(string); ok {
			out = append(out, s)
		}
	}
	return out
}

func objects(v any) []map[string]any {
	raw, _ := v.([]any)
	out := make([]map[string]any, 0, len(raw))
	for _, x := range raw {
		if m, ok := x.(map[string]any); ok {
			out = append(out, m)
		}
	}
	return out
}

// page applies `limit` and `page` to a list.
func page[T any](r *http.Request, items []T, defaultLimit int) []T {
	limit, _ := strconv.Atoi(r.URL.Query().Get("limit"))
	if limit <= 0 {
		limit = defaultLimit
	}
	n, _ := strconv.Atoi(r.URL.Query().Get("page"))
	if n <= 0 {
		n = 1
	}
	start := (n - 1) * limit
	if start >= len(items) {
		return []T{}
	}
	return items[start:min(start+limit, len(items))]
}

type handler func(w http.ResponseWriter, r *http.Request, body map[string]any)

func (f *fakeMux) routes() http.Handler {
	mux := http.NewServeMux()
	handle := func(pattern string, h handler) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			f.mu.Lock()
			defer f.mu.Unlock()
			f.requests = append(f.requests, r.Method+" "+r.URL.Path)
			if id, secret, ok := r.BasicAuth(); !ok || id != fakeTokenID || secret != fakeTokenSecret {
				writeError(w, http.StatusUnauthorized, "unauthorized", "Unauthorized request")
				return
			}
			if f.rateLimitNext > 0 {
				f.rateLimitNext--
				w.Header().Set("Retry-After", "0")
				w.WriteHeader(http.StatusTooManyRequests)
				_, _ = w.Write([]byte(`{"errors":["Too many requests, retry later."]}`))
				return
			}
			var body map[string]any
			if r.Body != nil {
				_ = json.NewDecoder(r.Body).Decode(&body)
			}
			h(w, r, body)
		})
	}

	handle("GET /system/v1/whoami", func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		writeData(w, 200, map[string]any{
			"organization_id": "org1", "organization_name": "Acme Inc",
			"environment_id": "env1", "environment_name": "Development", "environment_type": "development",
			"access_token_name": "terraform", "permissions": []string{"video:read", "video:write", "system:read", "system:write"},
		})
	})

	// ---- signing keys
	handle("POST /system/v1/signing-keys", func(w http.ResponseWriter, _ *http.Request, _ map[string]any) {
		k := &fakeSigningKey{ID: f.id("sk")}
		f.keys = append(f.keys, k)
		writeData(w, 201, map[string]any{"id": k.ID, "created_at": "1700000000", "private_key": "cHJpdmF0ZS1rZXktb2Yt" + k.ID})
	})
	handle("GET /system/v1/signing-keys", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		out := []map[string]any{}
		for _, k := range page(r, f.keys, 25) {
			out = append(out, map[string]any{"id": k.ID, "created_at": "1700000000"})
		}
		writeData(w, 200, out)
	})
	handle("GET /system/v1/signing-keys/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		id := r.PathValue("id")
		// A deleted key is still served from a cache.
		if find(f.keys, func(k *fakeSigningKey) string { return k.ID }, id) == nil && !slices.Contains(f.deletedKeys, id) {
			notFound(w, "Signing Key")
			return
		}
		writeData(w, 200, map[string]any{"id": id, "created_at": "1700000000"})
	})
	handle("DELETE /system/v1/signing-keys/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		before := len(f.keys)
		f.keys = slices.DeleteFunc(f.keys, func(k *fakeSigningKey) bool { return k.ID == r.PathValue("id") })
		if len(f.keys) == before {
			notFound(w, "Signing Key")
			return
		}
		f.deletedKeys = append(f.deletedKeys, r.PathValue("id"))
		w.WriteHeader(204)
	})

	// ---- webhooks
	webhookJSON := func(h *fakeWebhook) map[string]any {
		return map[string]any{"id": h.ID, "address": h.Address, "enabled": h.Enabled, "created_at": "2026-11-16T00:00:00.000000Z"}
	}
	addressTaken := func(address string, except *fakeWebhook) bool {
		return slices.ContainsFunc(f.webhooks, func(h *fakeWebhook) bool { return h != except && h.Address == address })
	}
	const missingWebhook = "The requested resource either doesn't exist or you don't have access to it."
	handle("POST /system/v1/webhooks", func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		address, _ := body["address"].(string)
		if addressTaken(address, nil) {
			writeError(w, 400, "bad_request", "A webhook with this address already exists in this environment.")
			return
		}
		h := &fakeWebhook{ID: f.id("wh"), Address: address, Enabled: true}
		f.webhooks = append(f.webhooks, h)
		out := webhookJSON(h)
		out["signing_secret"] = "secret-of-" + h.ID
		writeData(w, 201, out)
	})
	handle("GET /system/v1/webhooks/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		h := f.webhook(r.PathValue("id"))
		if h == nil {
			writeError(w, 404, "not_found", missingWebhook)
			return
		}
		writeData(w, 200, webhookJSON(h))
	})
	handle("PATCH /system/v1/webhooks/{id}", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		h := f.webhook(r.PathValue("id"))
		if h == nil {
			writeError(w, 404, "not_found", missingWebhook)
			return
		}
		address, hasAddress := body["address"].(string)
		enabled, hasEnabled := body["enabled"].(bool)
		if !hasAddress && !hasEnabled {
			writeError(w, 400, "bad_request", `Invalid parameters: at least one of "address" or "enabled" is required.`)
			return
		}
		if hasAddress {
			if addressTaken(address, h) {
				writeError(w, 400, "bad_request", "A webhook with this address already exists in this environment.")
				return
			}
			h.Address = address
		}
		if hasEnabled {
			h.Enabled = enabled
		}
		writeData(w, 200, webhookJSON(h))
	})
	handle("DELETE /system/v1/webhooks/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		before := len(f.webhooks)
		f.webhooks = slices.DeleteFunc(f.webhooks, func(h *fakeWebhook) bool { return h.ID == r.PathValue("id") })
		if len(f.webhooks) == before {
			writeError(w, 404, "not_found", missingWebhook)
			return
		}
		w.WriteHeader(204)
	})

	// ---- playback restrictions
	restrictionJSON := func(p *fakeRestriction) map[string]any {
		referrer := map[string]any{}
		if len(p.Domains) > 0 {
			referrer["allowed_domains"] = p.Domains
		}
		if p.AllowNoReferrer {
			referrer["allow_no_referrer"] = true
		}
		return map[string]any{
			"id": p.ID, "created_at": "1700000000", "updated_at": "1700000001", "referrer": referrer,
			"user_agent": map[string]any{"allow_no_user_agent": p.AllowNoUA, "allow_high_risk_user_agent": p.AllowHighRisk},
		}
	}
	setReferrer := func(w http.ResponseWriter, p *fakeRestriction, ref map[string]any) bool {
		domains := strs(ref["allowed_domains"])
		if len(domains) > 100 {
			invalid(w, fmt.Sprintf("Referrer Domain restrictions has %d entries (limit 100)", len(domains)))
			return false
		}
		for _, d := range domains {
			if strings.ContainsAny(d, " !/") {
				invalid(w, "Malformed referrer domain restriction allowlist entry: "+d)
				return false
			}
		}
		p.Domains = domains
		p.AllowNoReferrer, _ = ref["allow_no_referrer"].(bool)
		return true
	}
	setUserAgent := func(p *fakeRestriction, ua map[string]any) {
		if v, ok := ua["allow_no_user_agent"].(bool); ok {
			p.AllowNoUA = v
		}
		if v, ok := ua["allow_high_risk_user_agent"].(bool); ok {
			p.AllowHighRisk = v
		}
	}
	handle("POST /video/v1/playback-restrictions", func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		p := &fakeRestriction{ID: f.id("pr"), AllowNoUA: true, AllowHighRisk: true}
		ref, _ := body["referrer"].(map[string]any)
		if !setReferrer(w, p, ref) {
			return
		}
		ua, _ := body["user_agent"].(map[string]any)
		setUserAgent(p, ua)
		f.restrictions = append(f.restrictions, p)
		writeData(w, 201, restrictionJSON(p))
	})
	handle("GET /video/v1/playback-restrictions", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		out := []map[string]any{}
		for _, p := range page(r, f.restrictions, 25) {
			out = append(out, restrictionJSON(p))
		}
		writeData(w, 200, out)
	})
	handle("GET /video/v1/playback-restrictions/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		p := f.restriction(r.PathValue("id"))
		if p == nil {
			notFound(w, "Playback restriction")
			return
		}
		writeData(w, 200, restrictionJSON(p))
	})
	handle("PUT /video/v1/playback-restrictions/{id}/referrer", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		p := f.restriction(r.PathValue("id"))
		if p == nil {
			notFound(w, "Playback restriction")
			return
		}
		if setReferrer(w, p, body) {
			writeData(w, 200, restrictionJSON(p))
		}
	})
	handle("PUT /video/v1/playback-restrictions/{id}/user_agent", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		p := f.restriction(r.PathValue("id"))
		if p == nil {
			notFound(w, "Playback restriction")
			return
		}
		setUserAgent(p, body)
		writeData(w, 200, restrictionJSON(p))
	})
	handle("DELETE /video/v1/playback-restrictions/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		before := len(f.restrictions)
		f.restrictions = slices.DeleteFunc(f.restrictions, func(p *fakeRestriction) bool { return p.ID == r.PathValue("id") })
		if len(f.restrictions) == before {
			notFound(w, "Playback restriction")
			return
		}
		w.WriteHeader(204)
	})

	// ---- DRM configurations
	handle("GET /video/v1/drm-configurations", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		if len(f.drmConfigs) == 0 {
			writeData(w, 200, map[string]any{})
			return
		}
		out := []map[string]any{}
		for _, id := range page(r, f.drmConfigs, 25) {
			out = append(out, map[string]any{"id": id})
		}
		writeData(w, 200, out)
	})

	// ---- transcription vocabularies
	vocabularyJSON := func(v *fakeVocabulary) map[string]any {
		out := map[string]any{"id": v.ID, "created_at": "1700000000", "updated_at": "1700000001"}
		if v.Name != "" {
			out["name"] = v.Name
		}
		if v.Passthrough != "" {
			out["passthrough"] = v.Passthrough
		}
		if len(v.Phrases) > 0 {
			out["phrases"] = v.Phrases
		}
		return out
	}
	setVocabulary := func(v *fakeVocabulary, body map[string]any) {
		v.Name, _ = body["name"].(string)
		v.Passthrough, _ = body["passthrough"].(string)
		v.Phrases = strs(body["phrases"])
	}
	handle("POST /video/v1/transcription-vocabularies", func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		v := &fakeVocabulary{ID: f.id("tv")}
		setVocabulary(v, body)
		f.vocabularies = append(f.vocabularies, v)
		writeData(w, 201, vocabularyJSON(v))
	})
	handle("GET /video/v1/transcription-vocabularies/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		v := f.vocabulary(r.PathValue("id"))
		if v == nil {
			notFound(w, "Transcription Vocabulary")
			return
		}
		writeData(w, 200, vocabularyJSON(v))
	})
	handle("PUT /video/v1/transcription-vocabularies/{id}", func(w http.ResponseWriter, r *http.Request, body map[string]any) {
		v := f.vocabulary(r.PathValue("id"))
		if v == nil {
			notFound(w, "Transcription Vocabulary")
			return
		}
		setVocabulary(v, body)
		writeData(w, 200, vocabularyJSON(v))
	})
	handle("DELETE /video/v1/transcription-vocabularies/{id}", func(w http.ResponseWriter, r *http.Request, _ map[string]any) {
		before := len(f.vocabularies)
		f.vocabularies = slices.DeleteFunc(f.vocabularies, func(v *fakeVocabulary) bool { return v.ID == r.PathValue("id") })
		if len(f.vocabularies) == before {
			notFound(w, "Transcription Vocabulary")
			return
		}
		w.WriteHeader(204)
	})

	f.liveStreamRoutes(handle)
	return mux
}
