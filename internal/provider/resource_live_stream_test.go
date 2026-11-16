// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"regexp"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestLiveStreamLifecycle(t *testing.T) {
	f := newFakeMux(t)
	const name = "mux_live_stream.channel"
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_live_stream\" \"channel\" {\n" + body + "\n}\n"
	}
	var id, streamKey string
	same := resource.ComposeAggregateTestCheckFunc(
		resource.TestCheckResourceAttrWith(name, "id", unchanged(&id)),
		resource.TestCheckResourceAttrWith(name, "stream_key", unchanged(&streamKey)),
	)
	stream := func() *fakeStream { return snapshot(f, f.stream, id) }
	full := `title       = "Main channel"
  passthrough = "channel-1"
  latency_mode = "reduced"
  reconnect_window = 30.5
  max_continuous_duration = 3600
  use_slate_for_standard_latency = true
  reconnect_slate_url = "https://example.com/slate.png"
  new_asset_settings = {
    video_quality     = "plus"
    static_renditions = ["highest", "audio-only"]
  }
  embedded_subtitles = [{ name = "English CC" }]
  generated_subtitles = [{ name = "English (generated)", language_code = "en-US" }]`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if stream() != nil {
				return fmt.Errorf("live stream %s still exists", id)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				// With no arguments, Mux's defaults show up in the state.
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", remember(&id)),
					resource.TestCheckResourceAttrWith(name, "stream_key", remember(&streamKey)),
					resource.TestCheckResourceAttrSet(name, "srt_passphrase"),
					resource.TestCheckResourceAttr(name, "latency_mode", "standard"),
					resource.TestCheckResourceAttr(name, "reconnect_window", "60"),
					resource.TestCheckResourceAttr(name, "max_continuous_duration", "43200"),
					resource.TestCheckResourceAttr(name, "use_slate_for_standard_latency", "false"),
					resource.TestCheckResourceAttr(name, "audio_only", "false"),
					resource.TestCheckResourceAttr(name, "test", "false"),
					resource.TestCheckResourceAttr(name, "deletion_protection", "true"),
					resource.TestCheckNoResourceAttr(name, "new_asset_settings"),
					resource.TestCheckNoResourceAttr(name, "generated_subtitles"),
				),
			},
			{Config: config(""), ConfigPlanChecks: expectNoChanges},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_protection"},
			},
			{
				// Everything that can change in place does, over four endpoints.
				Config:           config(full),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(same,
					resource.TestCheckResourceAttr(name, "embedded_subtitles.0.language_code", "en"),
					resource.TestCheckResourceAttr(name, "embedded_subtitles.0.language_channel", "cc1"),
					func(*terraform.State) error {
						s := stream()
						got := fmt.Sprintln(s.Title, s.Passthrough, s.LatencyMode, s.ReconnectWindow, s.MaxContinuousDuration, s.UseSlate, s.SlateURL, s.VideoQuality)
						if want := "Main channel channel-1 reduced 30.5 3600 true https://example.com/slate.png plus\n"; got != want {
							return fmt.Errorf("stream = %s, want %s", got, want)
						}
						if len(s.Embedded) != 1 || len(s.Generated) != 1 || s.Generated[0]["language_code"] != "en-US" {
							return fmt.Errorf("subtitles = %v / %v", s.Embedded, s.Generated)
						}
						if r := slices.Sorted(slices.Values(s.StaticRenditions)); !slices.Equal(r, []string{"audio-only", "highest"}) {
							return fmt.Errorf("static renditions = %v", r)
						}
						return nil
					}),
			},
			{Config: config(full), ConfigPlanChecks: expectNoChanges},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_protection"},
			},
			{
				// Going to low latency while dropping the subtitles: Mux
				// refuses either order but one.
				Config: config(`latency_mode = "low"
  new_asset_settings = {
    static_renditions = ["highest"]
  }
  embedded_subtitles = []`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(same,
					resource.TestCheckNoResourceAttr(name, "title"),
					resource.TestCheckNoResourceAttr(name, "passthrough"),
					resource.TestCheckNoResourceAttr(name, "reconnect_slate_url"),
					resource.TestCheckResourceAttr(name, "embedded_subtitles.#", "0"),
					// Still what Mux has: unsetting them does not reset them.
					resource.TestCheckResourceAttr(name, "reconnect_window", "30.5"),
					resource.TestCheckResourceAttr(name, "new_asset_settings.video_quality", "plus"),
					func(*terraform.State) error {
						s := stream()
						if s.LatencyMode != "low" || s.Title != "" || s.Passthrough != "" || s.SlateURL != "" || len(s.Embedded)+len(s.Generated) != 0 {
							return fmt.Errorf("stream = %+v", s)
						}
						if !slices.Equal(s.StaticRenditions, []string{"highest"}) {
							return fmt.Errorf("static renditions = %v", s.StaticRenditions)
						}
						return nil
					}),
			},
			{
				// No static renditions is a DELETE: Mux refuses an empty list.
				Config: config(`latency_mode = "low"
  new_asset_settings = {}
  embedded_subtitles = []`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(same, func(*terraform.State) error {
					if s := stream(); len(s.StaticRenditions) != 0 {
						return fmt.Errorf("static renditions = %v", s.StaticRenditions)
					}
					return nil
				}),
			},
			{
				// Changed in the dashboard: drift, put back.
				PreConfig: func() {
					f.mutate(func() { f.stream(id).LatencyMode = "standard"; f.stream(id).Passthrough = "by-hand" })
				},
				Config: config(`latency_mode = "low"
  new_asset_settings = {}
  embedded_subtitles = []`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(same, func(*terraform.State) error {
					if s := stream(); s.LatencyMode != "low" || s.Passthrough != "" {
						return fmt.Errorf("stream = %+v", s)
					}
					return nil
				}),
			},
			{
				// The playback policies of new assets cannot change, so this
				// would replace the stream, which the protection refuses.
				Config: config(`latency_mode = "low"
  new_asset_settings = {
    playback_policies = ["signed"]
  }
  embedded_subtitles = []`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionReplace),
				ExpectError:      regexp.MustCompile(`Deletion protection is enabled`),
			},
			{
				// Mux ignores audio_only on an update, so that replaces too.
				Config: config(`latency_mode = "low"
  audio_only   = true
  new_asset_settings = {}
  embedded_subtitles = []`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionReplace),
				ExpectError:      regexp.MustCompile(`Deletion protection is enabled`),
			},
			{
				Config: config(`latency_mode = "low"
  new_asset_settings = {}
  embedded_subtitles = []
  deletion_protection = false`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check:            same,
			},
			{
				// Unprotected, the replacement goes through.
				Config:           config("audio_only = true\n  deletion_protection = false"),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionReplace),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", func(v string) error {
						if v == id {
							return fmt.Errorf("the live stream was not replaced")
						}
						id = v
						return nil
					}),
					resource.TestCheckResourceAttr(name, "latency_mode", "standard"),
				),
			},
			{
				// Deleted in the dashboard: recreated.
				PreConfig:        func() { f.mutate(func() { f.streams = nil }) },
				Config:           config("audio_only = true\n  deletion_protection = false"),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionCreate),
				Check:            resource.TestCheckResourceAttrWith(name, "id", remember(&id)),
			},
		},
	})
}

func TestLiveStreamTestMode(t *testing.T) {
	f := newFakeMux(t)
	const name = "mux_live_stream.trial"
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_live_stream\" \"trial\" {\n  test = true\n  deletion_protection = false\n" + body + "\n}\n"
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Mux would silently keep 300, leaving the result inconsistent.
				Config:      config("max_continuous_duration = 3600"),
				ExpectError: regexp.MustCompile(`Not available on a test live stream`),
			},
			{
				Config: config(""),
				Check:  resource.TestCheckResourceAttr(name, "max_continuous_duration", "300"),
			},
			{
				// An update must leave the duration out, or Mux refuses it.
				Config:           config(`title = "Trial"`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check:            resource.TestCheckResourceAttr(name, "max_continuous_duration", "300"),
			},
			{
				// A failed update leaves the state as Mux has it.
				Config: config(`title = "Renamed"
  latency_mode = "low"
  generated_subtitles = [{ name = "EN" }]`),
				ExpectError: regexp.MustCompile(`(?s)Unable to set the generated subtitles.*Low Latency cannot be used`),
			},
			{
				Config: config(`title = "Renamed"
  latency_mode = "low"`),
				ConfigPlanChecks: expectNoChanges,
			},
			{
				Config:      config(`generated_subtitles = [{ name = "EN" }, { name = "ES", language_code = "es" }]`),
				ExpectError: regexp.MustCompile(`at most 1`),
			},
			{
				Config: config(`title = "Renamed"
  latency_mode = "low"`),
			},
		},
	})
}

func TestLiveStreamChildren(t *testing.T) {
	f := newFakeMux(t)
	f.mutate(func() { f.drmConfigs = []string{"drm1"} })
	config := func(policy, simulcast string) string {
		return f.providerConfig() + `
resource "mux_transcription_vocabulary" "terms" {
  phrases = ["Mux"]
}

resource "mux_live_stream" "channel" {
  deletion_protection = false
  generated_subtitles = [{
    name                         = "English (generated)"
    transcription_vocabulary_ids = [mux_transcription_vocabulary.terms.id]
  }]
}

resource "mux_live_stream_playback_id" "watch" {
  live_stream_id = mux_live_stream.channel.id
  ` + policy + `
}

resource "mux_live_stream_simulcast_target" "restream" {
  live_stream_id = mux_live_stream.channel.id
  ` + simulcast + `
}
`
	}
	const (
		playback = "mux_live_stream_playback_id.watch"
		target   = "mux_live_stream_simulcast_target.restream"
	)
	first := config(`policy = "public"`, "url = \"rtmp://live.example.com/app\"\n  stream_key = \"abc\"")
	var streamID, playbackID, targetID string
	childID := func(resourceName string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			rs := s.RootModule().Resources[resourceName]
			return rs.Primary.Attributes["live_stream_id"] + "/" + rs.Primary.ID, nil
		}
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			f.mu.Lock()
			defer f.mu.Unlock()
			if len(f.streams)+len(f.vocabularies) != 0 {
				return fmt.Errorf("%d live streams and %d vocabularies are left", len(f.streams), len(f.vocabularies))
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("mux_live_stream.channel", "id", remember(&streamID)),
					resource.TestCheckResourceAttrWith(playback, "id", remember(&playbackID)),
					resource.TestCheckResourceAttrWith(target, "id", remember(&targetID)),
					resource.TestCheckResourceAttrPair("mux_live_stream.channel", "generated_subtitles.0.transcription_vocabulary_ids.0", "mux_transcription_vocabulary.terms", "id"),
					func(*terraform.State) error {
						s := snapshot(f, f.stream, streamID)
						if len(s.PlaybackIDs) != 1 || len(s.Simulcasts) != 1 || s.Simulcasts[0].StreamKey != "abc" {
							return fmt.Errorf("stream has %d playback IDs and %d simulcast targets", len(s.PlaybackIDs), len(s.Simulcasts))
						}
						return nil
					},
				),
			},
			{Config: first, ConfigPlanChecks: expectNoChanges},
			{ResourceName: playback, ImportState: true, ImportStateIdFunc: childID(playback), ImportStateVerify: true},
			{ResourceName: target, ImportState: true, ImportStateIdFunc: childID(target), ImportStateVerify: true},
			{
				ResourceName:  playback,
				ImportState:   true,
				ImportStateId: "no-slash",
				ExpectError:   regexp.MustCompile(`Invalid import ID`),
			},
			{
				// Neither has an update endpoint: a change replaces them.
				Config: config(`policy = "signed"`, `url = "rtmps://live.example.net/app"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(playback, plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction(target, plancheck.ResourceActionReplace),
					plancheck.ExpectResourceAction("mux_live_stream.channel", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(playback, "policy", "signed"),
					resource.TestCheckNoResourceAttr(target, "stream_key"),
					func(*terraform.State) error {
						s := snapshot(f, f.stream, streamID)
						if len(s.PlaybackIDs) != 1 || s.PlaybackIDs[0].ID == playbackID || len(s.Simulcasts) != 1 || s.Simulcasts[0].ID == targetID {
							return fmt.Errorf("the playback ID or the simulcast target was not replaced")
						}
						return nil
					},
				),
			},
			{
				Config: config("policy = \"drm\"\n  drm_configuration_id = \"drm1\"", `url = "rtmps://live.example.net/app"`),
				Check:  resource.TestCheckResourceAttr(playback, "drm_configuration_id", "drm1"),
			},
			{
				Config:      config(`policy = "drm"`, `url = "rtmps://live.example.net/app"`),
				ExpectError: regexp.MustCompile(`Missing DRM configuration`),
			},
			{
				// Mux would accept this and drop the configuration.
				Config:      config("policy = \"public\"\n  drm_configuration_id = \"drm1\"", `url = "rtmps://live.example.net/app"`),
				ExpectError: regexp.MustCompile(`Unexpected DRM configuration`),
			},
			{
				// Both removed in the dashboard: recreated.
				PreConfig: func() {
					f.mutate(func() { f.stream(streamID).PlaybackIDs, f.stream(streamID).Simulcasts = nil, nil })
				},
				Config: config("policy = \"drm\"\n  drm_configuration_id = \"drm1\"", `url = "rtmps://live.example.net/app"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction(playback, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(target, plancheck.ResourceActionCreate),
				}},
			},
			{
				// The whole live stream deleted in the dashboard takes its
				// children with it; all three come back.
				PreConfig: func() { f.mutate(func() { f.streams = nil }) },
				Config:    config("policy = \"drm\"\n  drm_configuration_id = \"drm1\"", `url = "rtmps://live.example.net/app"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("mux_live_stream.channel", plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(playback, plancheck.ResourceActionCreate),
					plancheck.ExpectResourceAction(target, plancheck.ResourceActionCreate),
				}},
			},
		},
	})
}
