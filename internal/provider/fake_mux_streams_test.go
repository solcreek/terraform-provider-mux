// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"net/http"
	"slices"
)

func (s *fakeStream) json() map[string]any {
	out := map[string]any{
		"id": s.ID, "created_at": "1700000000", "stream_key": s.StreamKey, "srt_passphrase": "srt-" + s.ID,
		"status": "idle", "latency_mode": s.LatencyMode, "reconnect_window": s.ReconnectWindow,
		"max_continuous_duration": s.MaxContinuousDuration,
	}
	if s.Active {
		out["status"] = "active"
	}
	text := func(key, v string) {
		if v != "" {
			out[key] = v
		}
	}
	flag := func(key string, v bool) {
		if v {
			out[key] = true
		}
	}
	text("passthrough", s.Passthrough)
	text("reconnect_slate_url", s.SlateURL)
	flag("use_slate_for_standard_latency", s.UseSlate)
	flag("audio_only", s.AudioOnly)
	flag("test", s.Test)
	flag("low_latency", s.LatencyMode == "low")
	if s.Title != "" {
		out["meta"] = map[string]any{"title": s.Title}
	}
	settings := map[string]any{}
	if len(s.AssetPolicies) > 0 {
		settings["playback_policies"] = s.AssetPolicies
	}
	if s.VideoQuality != "" {
		settings["video_quality"] = s.VideoQuality
	}
	if len(s.StaticRenditions) > 0 {
		renditions := []map[string]any{}
		for _, r := range s.StaticRenditions {
			renditions = append(renditions, map[string]any{"resolution": r})
		}
		settings["static_renditions"] = renditions
	}
	if len(settings) > 0 {
		out["new_asset_settings"] = settings
	}
	if len(s.Embedded) > 0 {
		out["embedded_subtitles"] = s.Embedded
	}
	if len(s.Generated) > 0 {
		out["generated_subtitles"] = s.Generated
	}
	if len(s.PlaybackIDs) > 0 {
		ids := []map[string]any{}
		for _, p := range s.PlaybackIDs {
			ids = append(ids, p.json())
		}
		out["playback_ids"] = ids
	}
	if len(s.Simulcasts) > 0 {
		targets := []map[string]any{}
		for _, t := range s.Simulcasts {
			targets = append(targets, t.json())
		}
		out["simulcast_targets"] = targets
	}
	return out
}

func (p *fakePlaybackID) json() map[string]any {
	out := map[string]any{"id": p.ID, "policy": p.Policy}
	if p.DRMConfigurationID != "" {
		out["drm_configuration_id"] = p.DRMConfigurationID
	}
	return out
}

func (t *fakeSimulcast) json() map[string]any {
	out := map[string]any{"id": t.ID, "url": t.URL, "status": "idle"}
	if t.StreamKey != "" {
		out["stream_key"] = t.StreamKey
	}
	if t.Passthrough != "" {
		out["passthrough"] = t.Passthrough
	}
	return out
}

// subtitleDefaults fills in what Mux does: language "en", and channel "cc1"
// for embedded captions. Empty values are dropped.
func subtitleDefaults(tracks []map[string]any, embedded bool) []map[string]any {
	for _, t := range tracks {
		if s, _ := t["language_code"].(string); s == "" {
			t["language_code"] = "en"
		}
		if s, _ := t["language_channel"].(string); embedded && s == "" {
			t["language_channel"] = "cc1"
		}
		for k, v := range t {
			if s, ok := v.(string); ok && s == "" {
				delete(t, k)
			}
			if l, ok := v.([]any); ok && len(l) == 0 {
				delete(t, k)
			}
		}
	}
	return tracks
}

func (f *fakeMux) liveStreamRoutes(handle func(pattern string, h handler)) {
	const base = "/video/v1/live-streams"
	withStream := func(h func(w http.ResponseWriter, r *http.Request, s *fakeStream, body map[string]any)) handler {
		return func(w http.ResponseWriter, r *http.Request, body map[string]any) {
			s := f.stream(r.PathValue("id"))
			if s == nil {
				notFound(w, "Live Stream")
				return
			}
			h(w, r, s, body)
		}
	}

	handle("POST "+base, func(w http.ResponseWriter, _ *http.Request, body map[string]any) {
		s := &fakeStream{ID: f.id("ls"), LatencyMode: "standard", ReconnectWindow: 60, MaxContinuousDuration: 43200}
		s.StreamKey = "key-" + s.ID
		if v, ok := body["latency_mode"].(string); ok {
			s.LatencyMode = v
		}
		if v, ok := body["reconnect_window"].(float64); ok {
			s.ReconnectWindow = v
		}
		if v, ok := body["max_continuous_duration"].(float64); ok {
			s.MaxContinuousDuration = int64(v)
		}
		s.Passthrough, _ = body["passthrough"].(string)
		s.SlateURL, _ = body["reconnect_slate_url"].(string)
		s.UseSlate, _ = body["use_slate_for_standard_latency"].(bool)
		s.AudioOnly, _ = body["audio_only"].(bool)
		s.Test, _ = body["test"].(bool)
		if s.Test {
			s.MaxContinuousDuration = 300
		}
		if meta, ok := body["meta"].(map[string]any); ok {
			s.Title, _ = meta["title"].(string)
		}
		if settings, ok := body["new_asset_settings"].(map[string]any); ok {
			if _, has := settings["passthrough"]; has {
				invalid(w, `The field "new_asset_settings.passthrough" should be provided as "passthrough" and will returned for both the live stream and its assets.`)
				return
			}
			s.AssetPolicies = strs(settings["playback_policies"])
			s.VideoQuality, _ = settings["video_quality"].(string)
			if s.VideoQuality == "basic" {
				invalid(w, "Live streams must have video quality 'plus' or 'premium'")
				return
			}
			for _, r := range objects(settings["static_renditions"]) {
				resolution, _ := r["resolution"].(string)
				s.StaticRenditions = append(s.StaticRenditions, resolution)
			}
		}
		s.Embedded = subtitleDefaults(objects(body["embedded_subtitles"]), true)
		s.Generated = subtitleDefaults(objects(body["generated_subtitles"]), false)
		if s.LatencyMode == "low" && len(s.Generated) > 0 {
			invalid(w, "Low Latency cannot be used with generated subtitles at this time")
			return
		}
		if s.LatencyMode == "low" && len(s.Embedded) > 0 {
			invalid(w, "Low Latency cannot be used with embedded subtitles at this time")
			return
		}
		if len(s.Generated) > 1 {
			invalid(w, "Only one live subtitle setting is allowed at this time.")
			return
		}
		for _, policy := range strs(body["playback_policies"]) {
			s.PlaybackIDs = append(s.PlaybackIDs, &fakePlaybackID{ID: f.id("pb"), Policy: policy})
		}
		f.streams = append(f.streams, s)
		writeData(w, 201, s.json())
	})

	handle("GET "+base+"/{id}", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, _ map[string]any) {
		writeData(w, 200, s.json())
	}))

	handle("PATCH "+base+"/{id}", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		if settings, ok := body["new_asset_settings"].(map[string]any); ok {
			for k := range settings {
				if k != "video_quality" && k != "mp4_support" && k != "master_access" {
					invalid(w, "Only mp4_support, master_access, and video_quality fields may be updated in new_asset_settings for a live stream")
					return
				}
			}
			if q, ok := settings["video_quality"].(string); ok {
				if q != "plus" && q != "premium" {
					invalid(w, "Live streams must have video quality 'plus' or 'premium'")
					return
				}
				s.VideoQuality = q
			}
		}
		if _, ok := body["max_continuous_duration"]; ok && s.Test {
			invalid(w, "Cannot use max continuous duration with test live streams")
			return
		}
		if v, ok := body["latency_mode"].(string); ok {
			if v == "low" && len(s.Generated) > 0 {
				invalid(w, "Livestreams with generated subtitles may not be Low Latency.")
				return
			}
			if v == "low" && len(s.Embedded) > 0 {
				invalid(w, "Livestreams with embedded subtitles may not be Low Latency.")
				return
			}
			s.LatencyMode = v
		}
		if v, ok := body["reconnect_window"].(float64); ok {
			s.ReconnectWindow = v
		}
		if v, ok := body["max_continuous_duration"].(float64); ok {
			s.MaxContinuousDuration = int64(v)
		}
		if v, ok := body["use_slate_for_standard_latency"].(bool); ok {
			s.UseSlate = v
		}
		if v, ok := body["passthrough"].(string); ok {
			s.Passthrough = v
		}
		if v, ok := body["reconnect_slate_url"].(string); ok {
			s.SlateURL = v
		}
		if meta, ok := body["meta"].(map[string]any); ok {
			if v, ok := meta["title"].(string); ok {
				s.Title = v
			}
		}
		// audio_only and test are accepted and ignored.
		writeData(w, 200, s.json())
	}))

	handle("DELETE "+base+"/{id}", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, _ map[string]any) {
		f.streams = slices.DeleteFunc(f.streams, func(x *fakeStream) bool { return x == s })
		w.WriteHeader(204)
	}))

	handle("PUT "+base+"/{id}/embedded-subtitles", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		if s.LatencyMode == "low" {
			invalid(w, "Low Latency cannot be used with embedded subtitles at this time")
			return
		}
		s.Embedded = subtitleDefaults(objects(body["embedded_subtitles"]), true)
		writeData(w, 200, s.json())
	}))
	handle("PUT "+base+"/{id}/generated-subtitles", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		if s.LatencyMode == "low" {
			invalid(w, "Low Latency cannot be used with generated subtitles at this time")
			return
		}
		tracks := objects(body["generated_subtitles"])
		if len(tracks) > 1 {
			invalid(w, "Only one live subtitle setting is allowed at this time.")
			return
		}
		s.Generated = subtitleDefaults(tracks, false)
		writeData(w, 200, s.json())
	}))

	handle("PUT "+base+"/{id}/new-asset-settings/static-renditions", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		renditions := objects(body["static_renditions"])
		if len(renditions) == 0 {
			invalid(w, "No static renditions are specified")
			return
		}
		s.StaticRenditions = nil
		for _, r := range renditions {
			resolution, _ := r["resolution"].(string)
			s.StaticRenditions = append(s.StaticRenditions, resolution)
		}
		writeData(w, 200, s.json())
	}))
	handle("DELETE "+base+"/{id}/new-asset-settings/static-renditions", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, _ map[string]any) {
		s.StaticRenditions = nil
		w.WriteHeader(204)
	}))

	// ---- playback IDs
	handle("POST "+base+"/{id}/playback-ids", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		p := &fakePlaybackID{ID: f.id("pb")}
		p.Policy, _ = body["policy"].(string)
		drm, _ := body["drm_configuration_id"].(string)
		if p.Policy == "drm" {
			if !slices.Contains(f.drmConfigs, drm) {
				invalid(w, `Failed to parse ID "`+drm+`"`)
				return
			}
			p.DRMConfigurationID = drm
		}
		s.PlaybackIDs = append(s.PlaybackIDs, p)
		writeData(w, 201, p.json())
	}))
	handle("GET "+base+"/{id}/playback-ids/{child}", withStream(func(w http.ResponseWriter, r *http.Request, s *fakeStream, _ map[string]any) {
		p := find(s.PlaybackIDs, func(p *fakePlaybackID) string { return p.ID }, r.PathValue("child"))
		if p == nil {
			notFound(w, "Playback ID")
			return
		}
		writeData(w, 200, p.json())
	}))
	handle("DELETE "+base+"/{id}/playback-ids/{child}", withStream(func(w http.ResponseWriter, r *http.Request, s *fakeStream, _ map[string]any) {
		before := len(s.PlaybackIDs)
		s.PlaybackIDs = slices.DeleteFunc(s.PlaybackIDs, func(p *fakePlaybackID) bool { return p.ID == r.PathValue("child") })
		if len(s.PlaybackIDs) == before {
			notFound(w, "Playback ID")
			return
		}
		w.WriteHeader(204)
	}))

	// ---- simulcast targets
	handle("POST "+base+"/{id}/simulcast-targets", withStream(func(w http.ResponseWriter, _ *http.Request, s *fakeStream, body map[string]any) {
		if s.Test {
			invalid(w, "Simulcasts are not supported for test streams")
			return
		}
		if s.Active {
			// Documented by Mux; the message is not an observed one.
			invalid(w, "Simulcast targets can only be created when the live stream is idle")
			return
		}
		t := &fakeSimulcast{ID: f.id("st")}
		t.URL, _ = body["url"].(string)
		t.StreamKey, _ = body["stream_key"].(string)
		t.Passthrough, _ = body["passthrough"].(string)
		s.Simulcasts = append(s.Simulcasts, t)
		writeData(w, 201, t.json())
	}))
	handle("GET "+base+"/{id}/simulcast-targets/{child}", withStream(func(w http.ResponseWriter, r *http.Request, s *fakeStream, _ map[string]any) {
		t := find(s.Simulcasts, func(t *fakeSimulcast) string { return t.ID }, r.PathValue("child"))
		if t == nil {
			notFound(w, "Simulcast target")
			return
		}
		writeData(w, 200, t.json())
	}))
	handle("DELETE "+base+"/{id}/simulcast-targets/{child}", withStream(func(w http.ResponseWriter, r *http.Request, s *fakeStream, _ map[string]any) {
		before := len(s.Simulcasts)
		s.Simulcasts = slices.DeleteFunc(s.Simulcasts, func(t *fakeSimulcast) bool { return t.ID == r.PathValue("child") })
		if len(s.Simulcasts) == before {
			notFound(w, "Simulcast target")
			return
		}
		w.WriteHeader(204)
	}))
}
