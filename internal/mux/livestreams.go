// SPDX-License-Identifier: MPL-2.0

package mux

import (
	"context"
	"net/http"
)

// PlaybackID is an ID viewers play a live stream through.
type PlaybackID struct {
	ID                 string `json:"id,omitempty"`
	Policy             string `json:"policy"`
	DRMConfigurationID string `json:"drm_configuration_id,omitempty"`
}

// StaticRendition is a downloadable rendition made of each recorded asset.
type StaticRendition struct {
	Resolution string `json:"resolution"`
}

// NewAssetSettings configures the assets Mux records from a live stream.
// Mux leaves the whole object out of a stream that sets none of it.
type NewAssetSettings struct {
	PlaybackPolicies  []string          `json:"playback_policies,omitempty"`
	VideoQuality      string            `json:"video_quality,omitempty"`
	StaticRenditions  []StaticRendition `json:"static_renditions,omitempty"`
	MaxResolutionTier string            `json:"max_resolution_tier,omitempty"`
}

// IsZero reports whether no setting is present.
func (s *NewAssetSettings) IsZero() bool {
	return s == nil || (len(s.PlaybackPolicies) == 0 && s.VideoQuality == "" && len(s.StaticRenditions) == 0)
}

// EmbeddedSubtitles describes closed captions embedded in the incoming
// stream (CEA-608).
type EmbeddedSubtitles struct {
	Name            string `json:"name,omitempty"`
	Passthrough     string `json:"passthrough,omitempty"`
	LanguageCode    string `json:"language_code,omitempty"`
	LanguageChannel string `json:"language_channel,omitempty"`
}

// GeneratedSubtitles configures subtitles made by speech recognition.
type GeneratedSubtitles struct {
	Name                       string   `json:"name,omitempty"`
	Passthrough                string   `json:"passthrough,omitempty"`
	LanguageCode               string   `json:"language_code,omitempty"`
	TranscriptionVocabularyIDs []string `json:"transcription_vocabulary_ids,omitempty"`
}

// Meta is the metadata of a live stream that players may show.
type Meta struct {
	Title string `json:"title"`
}

// LiveStream is a Mux live stream. Mux leaves out every field that is unset
// or false.
type LiveStream struct {
	ID string `json:"id"`
	// CreatedAt is in seconds since the Unix epoch.
	CreatedAt string `json:"created_at"`
	// StreamKey and SRTPassphrase are credentials: whoever has them can
	// broadcast to the stream.
	StreamKey                  string               `json:"stream_key"`
	SRTPassphrase              string               `json:"srt_passphrase"`
	Status                     string               `json:"status"`
	LatencyMode                string               `json:"latency_mode"`
	ReconnectWindow            float64              `json:"reconnect_window"`
	MaxContinuousDuration      int64                `json:"max_continuous_duration"`
	UseSlateForStandardLatency bool                 `json:"use_slate_for_standard_latency"`
	ReconnectSlateURL          string               `json:"reconnect_slate_url"`
	Passthrough                string               `json:"passthrough"`
	AudioOnly                  bool                 `json:"audio_only"`
	Test                       bool                 `json:"test"`
	Meta                       *Meta                `json:"meta"`
	NewAssetSettings           *NewAssetSettings    `json:"new_asset_settings"`
	EmbeddedSubtitles          []EmbeddedSubtitles  `json:"embedded_subtitles"`
	GeneratedSubtitles         []GeneratedSubtitles `json:"generated_subtitles"`
	PlaybackIDs                []PlaybackID         `json:"playback_ids"`
	SimulcastTargets           []SimulcastTarget    `json:"simulcast_targets"`
}

// Title returns the stream's title, or "".
func (l *LiveStream) Title() string {
	if l.Meta == nil {
		return ""
	}
	return l.Meta.Title
}

// LiveStreamCreate is the create payload. Zero fields are left out, so Mux
// applies its defaults.
type LiveStreamCreate struct {
	LatencyMode                string               `json:"latency_mode,omitempty"`
	ReconnectWindow            *float64             `json:"reconnect_window,omitempty"`
	MaxContinuousDuration      *int64               `json:"max_continuous_duration,omitempty"`
	UseSlateForStandardLatency bool                 `json:"use_slate_for_standard_latency,omitempty"`
	ReconnectSlateURL          string               `json:"reconnect_slate_url,omitempty"`
	Passthrough                string               `json:"passthrough,omitempty"`
	AudioOnly                  bool                 `json:"audio_only,omitempty"`
	Test                       bool                 `json:"test,omitempty"`
	Meta                       *Meta                `json:"meta,omitempty"`
	NewAssetSettings           *NewAssetSettings    `json:"new_asset_settings,omitempty"`
	EmbeddedSubtitles          []EmbeddedSubtitles  `json:"embedded_subtitles,omitempty"`
	GeneratedSubtitles         []GeneratedSubtitles `json:"generated_subtitles,omitempty"`
}

// LiveStreamUpdate is the PATCH payload; nil fields are left as they are.
// An empty Passthrough, ReconnectSlateURL or title clears the value. Of the
// new asset settings Mux only lets the video quality change here.
type LiveStreamUpdate struct {
	LatencyMode                *string           `json:"latency_mode,omitempty"`
	ReconnectWindow            *float64          `json:"reconnect_window,omitempty"`
	MaxContinuousDuration      *int64            `json:"max_continuous_duration,omitempty"`
	UseSlateForStandardLatency *bool             `json:"use_slate_for_standard_latency,omitempty"`
	ReconnectSlateURL          *string           `json:"reconnect_slate_url,omitempty"`
	Passthrough                *string           `json:"passthrough,omitempty"`
	Meta                       *Meta             `json:"meta,omitempty"`
	NewAssetSettings           *NewAssetSettings `json:"new_asset_settings,omitempty"`
}

const liveStreams = "/video/v1/live-streams"

func (c *Client) liveStream(ctx context.Context, method, path string, body any) (*LiveStream, error) {
	var out LiveStream
	if err := c.do(ctx, method, path, nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// CreateLiveStream creates a live stream. Without playback policies it has
// no playback ID; add them with CreateLiveStreamPlaybackID.
func (c *Client) CreateLiveStream(ctx context.Context, in LiveStreamCreate) (*LiveStream, error) {
	return c.liveStream(ctx, http.MethodPost, liveStreams, in)
}

// GetLiveStream returns a live stream.
func (c *Client) GetLiveStream(ctx context.Context, id string) (*LiveStream, error) {
	return c.liveStream(ctx, http.MethodGet, liveStreams+"/"+seg(id), nil)
}

// UpdateLiveStream changes the settings named in upd.
func (c *Client) UpdateLiveStream(ctx context.Context, id string, upd LiveStreamUpdate) (*LiveStream, error) {
	return c.liveStream(ctx, http.MethodPatch, liveStreams+"/"+seg(id), upd)
}

// DeleteLiveStream deletes a live stream and ends any broadcast on it. The
// assets recorded from it are kept.
func (c *Client) DeleteLiveStream(ctx context.Context, id string) error {
	return c.do(ctx, http.MethodDelete, liveStreams+"/"+seg(id), nil, nil, nil)
}

// SetEmbeddedSubtitles replaces the embedded subtitle configuration; an
// empty list removes it. Mux refuses it on a low latency stream.
func (c *Client) SetEmbeddedSubtitles(ctx context.Context, id string, subs []EmbeddedSubtitles) (*LiveStream, error) {
	if subs == nil {
		subs = []EmbeddedSubtitles{}
	}
	body := map[string]any{"embedded_subtitles": subs}
	return c.liveStream(ctx, http.MethodPut, liveStreams+"/"+seg(id)+"/embedded-subtitles", body)
}

// SetGeneratedSubtitles replaces the generated subtitle configuration; an
// empty list removes it. Mux allows one track, and none on a low latency
// stream.
func (c *Client) SetGeneratedSubtitles(ctx context.Context, id string, subs []GeneratedSubtitles) (*LiveStream, error) {
	if subs == nil {
		subs = []GeneratedSubtitles{}
	}
	body := map[string]any{"generated_subtitles": subs}
	return c.liveStream(ctx, http.MethodPut, liveStreams+"/"+seg(id)+"/generated-subtitles", body)
}

// SetStaticRenditions replaces the static renditions made of new assets.
// Mux refuses an empty list on the update endpoint, so none is a delete.
func (c *Client) SetStaticRenditions(ctx context.Context, id string, resolutions []string) error {
	path := liveStreams + "/" + seg(id) + "/new-asset-settings/static-renditions"
	if len(resolutions) == 0 {
		return c.do(ctx, http.MethodDelete, path, nil, nil, nil)
	}
	renditions := make([]StaticRendition, 0, len(resolutions))
	for _, r := range resolutions {
		renditions = append(renditions, StaticRendition{Resolution: r})
	}
	return c.do(ctx, http.MethodPut, path, nil, map[string]any{"static_renditions": renditions}, nil)
}

// CreateLiveStreamPlaybackID adds a playback ID to a live stream.
func (c *Client) CreateLiveStreamPlaybackID(ctx context.Context, streamID, policy, drmConfigurationID string) (*PlaybackID, error) {
	var out PlaybackID
	body := PlaybackID{Policy: policy, DRMConfigurationID: drmConfigurationID}
	if err := c.do(ctx, http.MethodPost, liveStreams+"/"+seg(streamID)+"/playback-ids", nil, body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetLiveStreamPlaybackID returns one playback ID of a live stream.
func (c *Client) GetLiveStreamPlaybackID(ctx context.Context, streamID, id string) (*PlaybackID, error) {
	var out PlaybackID
	if err := c.do(ctx, http.MethodGet, liveStreams+"/"+seg(streamID)+"/playback-ids/"+seg(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteLiveStreamPlaybackID removes a playback ID. New playback through it
// fails at once.
func (c *Client) DeleteLiveStreamPlaybackID(ctx context.Context, streamID, id string) error {
	return c.do(ctx, http.MethodDelete, liveStreams+"/"+seg(streamID)+"/playback-ids/"+seg(id), nil, nil, nil)
}

// SimulcastTarget restreams a live stream to a third-party service.
type SimulcastTarget struct {
	ID  string `json:"id,omitempty"`
	URL string `json:"url"`
	// StreamKey is the third party's credential for the stream.
	StreamKey   string `json:"stream_key,omitempty"`
	Passthrough string `json:"passthrough,omitempty"`
	Status      string `json:"status,omitempty"`
}

// CreateSimulcastTarget adds a simulcast target. Mux only accepts it while
// the live stream is idle.
func (c *Client) CreateSimulcastTarget(ctx context.Context, streamID string, in SimulcastTarget) (*SimulcastTarget, error) {
	var out SimulcastTarget
	in.ID, in.Status = "", ""
	if err := c.do(ctx, http.MethodPost, liveStreams+"/"+seg(streamID)+"/simulcast-targets", nil, in, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetSimulcastTarget returns one simulcast target of a live stream.
func (c *Client) GetSimulcastTarget(ctx context.Context, streamID, id string) (*SimulcastTarget, error) {
	var out SimulcastTarget
	if err := c.do(ctx, http.MethodGet, liveStreams+"/"+seg(streamID)+"/simulcast-targets/"+seg(id), nil, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteSimulcastTarget removes a simulcast target. Mux only accepts it
// while the live stream is idle.
func (c *Client) DeleteSimulcastTarget(ctx context.Context, streamID, id string) error {
	return c.do(ctx, http.MethodDelete, liveStreams+"/"+seg(streamID)+"/simulcast-targets/"+seg(id), nil, nil, nil)
}
