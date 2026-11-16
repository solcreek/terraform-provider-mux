// SPDX-License-Identifier: MPL-2.0

package mux

import (
	"context"
	"net/http"
)

// ReferrerRestriction says which referring domains may play a video.
type ReferrerRestriction struct {
	// AllowedDomains is sent even when empty: an empty list denies every
	// domain, and ["*"] allows any.
	AllowedDomains  []string `json:"allowed_domains"`
	AllowNoReferrer bool     `json:"allow_no_referrer"`
}

// UserAgentRestriction says which user agents may play a video.
type UserAgentRestriction struct {
	AllowNoUserAgent       bool `json:"allow_no_user_agent"`
	AllowHighRiskUserAgent bool `json:"allow_high_risk_user_agent"`
}

// PlaybackRestriction is a set of rules a signed playback token refers to by
// ID. Mux leaves allowed_domains out when the list is empty and
// allow_no_referrer out when it is false.
type PlaybackRestriction struct {
	ID        string               `json:"id,omitempty"`
	CreatedAt string               `json:"created_at,omitempty"`
	UpdatedAt string               `json:"updated_at,omitempty"`
	Referrer  ReferrerRestriction  `json:"referrer"`
	UserAgent UserAgentRestriction `json:"user_agent"`
}

// CreatePlaybackRestriction creates a playback restriction.
func (c *Client) CreatePlaybackRestriction(ctx context.Context, ref ReferrerRestriction, ua UserAgentRestriction) (*PlaybackRestriction, error) {
	var out PlaybackRestriction
	body := PlaybackRestriction{Referrer: normalized(ref), UserAgent: ua}
	if err := c.do(ctx, http.MethodPost, "/video/v1/playback-restrictions", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetPlaybackRestriction returns a playback restriction.
func (c *Client) GetPlaybackRestriction(ctx context.Context, id string) (*PlaybackRestriction, error) {
	var out PlaybackRestriction
	if err := c.do(ctx, http.MethodGet, "/video/v1/playback-restrictions/"+seg(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateReferrerRestriction replaces the referrer rules of a restriction.
func (c *Client) UpdateReferrerRestriction(ctx context.Context, id string, ref ReferrerRestriction) (*PlaybackRestriction, error) {
	var out PlaybackRestriction
	if err := c.do(ctx, http.MethodPut, "/video/v1/playback-restrictions/"+seg(id)+"/referrer", nil, normalized(ref), &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateUserAgentRestriction replaces the user agent rules of a restriction.
func (c *Client) UpdateUserAgentRestriction(ctx context.Context, id string, ua UserAgentRestriction) (*PlaybackRestriction, error) {
	var out PlaybackRestriction
	if err := c.do(ctx, http.MethodPut, "/video/v1/playback-restrictions/"+seg(id)+"/user_agent", nil, ua, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeletePlaybackRestriction deletes a playback restriction.
func (c *Client) DeletePlaybackRestriction(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/video/v1/playback-restrictions/"+seg(id), nil, nil, nil)
}

// normalized makes a nil domain list encode as [] rather than null.
func normalized(ref ReferrerRestriction) ReferrerRestriction {
	if ref.AllowedDomains == nil {
		ref.AllowedDomains = []string{}
	}
	return ref
}

// DRMConfiguration is a DRM setup Mux provisioned for the environment. The
// API only reads them.
type DRMConfiguration struct {
	ID string `json:"id"`
}

// ListPlaybackRestrictions returns every playback restriction of the
// environment.
func (c *Client) ListPlaybackRestrictions(ctx context.Context) ([]PlaybackRestriction, error) {
	return list[PlaybackRestriction](ctx, c, "/video/v1/playback-restrictions", pageSize)
}

// ListDRMConfigurations returns every DRM configuration of the environment.
func (c *Client) ListDRMConfigurations(ctx context.Context) ([]DRMConfiguration, error) {
	return list[DRMConfiguration](ctx, c, "/video/v1/drm-configurations", pageSize)
}

// TranscriptionVocabulary is a list of phrases that improves the generated
// subtitles of the live streams it is attached to.
type TranscriptionVocabulary struct {
	ID          string   `json:"id,omitempty"`
	Name        string   `json:"name,omitempty"`
	Phrases     []string `json:"phrases"`
	Passthrough string   `json:"passthrough,omitempty"`
	CreatedAt   string   `json:"created_at,omitempty"`
}

// CreateTranscriptionVocabulary creates a vocabulary.
func (c *Client) CreateTranscriptionVocabulary(ctx context.Context, name string, phrases []string, passthrough string) (*TranscriptionVocabulary, error) {
	return c.writeVocabulary(ctx, http.MethodPost, "/video/v1/transcription-vocabularies", name, phrases, passthrough)
}

// GetTranscriptionVocabulary returns a vocabulary.
func (c *Client) GetTranscriptionVocabulary(ctx context.Context, id string) (*TranscriptionVocabulary, error) {
	var out TranscriptionVocabulary
	if err := c.do(ctx, http.MethodGet, "/video/v1/transcription-vocabularies/"+seg(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// UpdateTranscriptionVocabulary replaces a vocabulary: the update is a PUT,
// so a name or passthrough left empty is removed (observed 2026-11-16).
func (c *Client) UpdateTranscriptionVocabulary(ctx context.Context, id, name string, phrases []string, passthrough string) (*TranscriptionVocabulary, error) {
	return c.writeVocabulary(ctx, http.MethodPut, "/video/v1/transcription-vocabularies/"+seg(id), name, phrases, passthrough)
}

func (c *Client) writeVocabulary(ctx context.Context, method, path, name string, phrases []string, passthrough string) (*TranscriptionVocabulary, error) {
	if phrases == nil {
		phrases = []string{}
	}
	var out TranscriptionVocabulary
	body := TranscriptionVocabulary{Name: name, Phrases: phrases, Passthrough: passthrough}
	if err := c.do(ctx, method, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteTranscriptionVocabulary deletes a vocabulary and detaches it from
// the live streams that use it.
func (c *Client) DeleteTranscriptionVocabulary(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, "/video/v1/transcription-vocabularies/"+seg(id), nil, nil, nil)
}
