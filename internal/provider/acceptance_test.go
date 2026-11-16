// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"slices"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

// Acceptance tests call the real Mux API (see testAccPreCheck). Everything
// they create carries tf-acc-<random> in a name, address or domain and is
// deleted again. They never touch an object they did not create.

// TestAccLifecycle creates, imports, changes and destroys every resource.
func TestAccLifecycle(t *testing.T) {
	testAccPreCheck(t)
	suffix := randomSuffix()
	domain := "tf-acc-" + suffix + ".example.com"

	config := func(extraDomain, webhookPath string, webhookEnabled bool, stream string) string {
		return requiredProviders + fmt.Sprintf(`
data "mux_environment" "this" {}

data "mux_drm_configurations" "all" {}

resource "mux_playback_restriction" "acc" {
  referrer = {
    allowed_domains = ["%[1]s", "*.%[1]s"%[2]s]
  }
  deletion_protection = false
}

data "mux_playback_restrictions" "all" {
  depends_on = [mux_playback_restriction.acc]
}

resource "mux_signing_key" "acc" {
  deletion_protection = false
}

resource "mux_webhook" "acc" {
  address = "https://%[1]s/%[3]s"
  enabled = %[4]t
}

resource "mux_transcription_vocabulary" "acc" {
  name    = "tf-acc-%[5]s"
  phrases = ["Mux", "OpenTofu"]
}

resource "mux_live_stream" "acc" {
  deletion_protection = false
%[6]s
}

resource "mux_live_stream_playback_id" "acc" {
  live_stream_id = mux_live_stream.acc.id
  policy         = "public"
}

resource "mux_live_stream_simulcast_target" "acc" {
  live_stream_id = mux_live_stream.acc.id
  url            = "rtmp://%[1]s/app"
  stream_key     = "tf-acc"
}
`, domain, extraDomain, webhookPath, webhookEnabled, suffix, stream)
	}
	first := config("", "mux", true, `  title = "tf-acc-`+suffix+`"
  generated_subtitles = [{
    name                         = "English (generated)"
    transcription_vocabulary_ids = [mux_transcription_vocabulary.acc.id]
  }]`)
	changed := config(`, "added.`+domain+`"`, "hooks/mux", false, `  passthrough      = "tf-acc"
  latency_mode     = "low"
  reconnect_window = 30
  new_asset_settings = {
    video_quality     = "plus"
    static_renditions = ["highest"]
  }`)

	var restrictionID, keyID, webhookID, vocabularyID, streamID, streamKey, privateKey, secret string
	childID := func(name string) resource.ImportStateIdFunc {
		return func(s *terraform.State) (string, error) {
			rs := s.RootModule().Resources[name]
			return rs.Primary.Attributes["live_stream_id"] + "/" + rs.Primary.ID, nil
		}
	}

	resource.Test(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			client, ctx := testAccClient(), context.Background()
			checks := map[string]error{}
			_, checks["playback restriction "+restrictionID] = client.GetPlaybackRestriction(ctx, restrictionID)
			_, checks["signing key "+keyID] = client.GetSigningKey(ctx, keyID)
			_, checks["webhook "+webhookID] = client.GetWebhook(ctx, webhookID)
			_, checks["transcription vocabulary "+vocabularyID] = client.GetTranscriptionVocabulary(ctx, vocabularyID)
			_, checks["live stream "+streamID] = client.GetLiveStream(ctx, streamID)
			for what, err := range checks {
				if !mux.IsNotFound(err) {
					return fmt.Errorf("%s after destroy: err = %v, want a 404", what, err)
				}
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: first,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.mux_environment.this", "type", "development"),
					resource.TestCheckResourceAttrWith("mux_playback_restriction.acc", "id", remember(&restrictionID)),
					resource.TestCheckResourceAttrWith("mux_signing_key.acc", "id", remember(&keyID)),
					resource.TestCheckResourceAttrWith("mux_signing_key.acc", "private_key", remember(&privateKey)),
					resource.TestCheckResourceAttrWith("mux_webhook.acc", "id", remember(&webhookID)),
					resource.TestCheckResourceAttrWith("mux_webhook.acc", "signing_secret", remember(&secret)),
					resource.TestCheckResourceAttrWith("mux_transcription_vocabulary.acc", "id", remember(&vocabularyID)),
					resource.TestCheckResourceAttrWith("mux_live_stream.acc", "id", remember(&streamID)),
					resource.TestCheckResourceAttrWith("mux_live_stream.acc", "stream_key", remember(&streamKey)),
					resource.TestCheckResourceAttr("mux_playback_restriction.acc", "referrer.allowed_domains.#", "2"),
					resource.TestCheckResourceAttr("mux_playback_restriction.acc", "user_agent.allow_no_user_agent", "true"),
					resource.TestCheckResourceAttr("mux_live_stream.acc", "latency_mode", "standard"),
					resource.TestCheckResourceAttr("mux_live_stream.acc", "generated_subtitles.0.language_code", "en"),
					resource.TestCheckResourceAttrSet("mux_live_stream.acc", "srt_passphrase"),
					resource.TestCheckResourceAttrSet("mux_live_stream_playback_id.acc", "id"),
					resource.TestCheckResourceAttrWith("data.mux_playback_restrictions.all", "restrictions.#", func(v string) error {
						if v == "0" {
							return fmt.Errorf("the list of playback restrictions is empty")
						}
						return nil
					}),
				),
			},
			{Config: first, ConfigPlanChecks: expectNoChanges},
			{
				ResourceName: "mux_playback_restriction.acc", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"deletion_protection"},
			},
			{
				ResourceName: "mux_signing_key.acc", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"deletion_protection", "private_key"},
			},
			{
				ResourceName: "mux_webhook.acc", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
			{ResourceName: "mux_transcription_vocabulary.acc", ImportState: true, ImportStateVerify: true},
			{
				ResourceName: "mux_live_stream.acc", ImportState: true, ImportStateVerify: true,
				ImportStateVerifyIgnore: []string{"deletion_protection"},
			},
			{
				ResourceName: "mux_live_stream_playback_id.acc", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: childID("mux_live_stream_playback_id.acc"),
			},
			{
				ResourceName: "mux_live_stream_simulcast_target.acc", ImportState: true, ImportStateVerify: true,
				ImportStateIdFunc: childID("mux_live_stream_simulcast_target.acc"),
			},
			{
				// Everything changes in place: no ID, key or secret moves.
				Config: changed,
				ConfigPlanChecks: resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{
					plancheck.ExpectResourceAction("mux_playback_restriction.acc", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("mux_webhook.acc", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("mux_live_stream.acc", plancheck.ResourceActionUpdate),
					plancheck.ExpectResourceAction("mux_signing_key.acc", plancheck.ResourceActionNoop),
				}},
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith("mux_playback_restriction.acc", "id", unchanged(&restrictionID)),
					resource.TestCheckResourceAttr("mux_playback_restriction.acc", "referrer.allowed_domains.#", "3"),
					resource.TestCheckResourceAttrWith("mux_webhook.acc", "id", unchanged(&webhookID)),
					resource.TestCheckResourceAttrWith("mux_webhook.acc", "signing_secret", unchanged(&secret)),
					resource.TestCheckResourceAttr("mux_webhook.acc", "enabled", "false"),
					resource.TestCheckResourceAttrWith("mux_signing_key.acc", "private_key", unchanged(&privateKey)),
					resource.TestCheckResourceAttrWith("mux_live_stream.acc", "id", unchanged(&streamID)),
					resource.TestCheckResourceAttrWith("mux_live_stream.acc", "stream_key", unchanged(&streamKey)),
					resource.TestCheckResourceAttr("mux_live_stream.acc", "latency_mode", "low"),
					resource.TestCheckResourceAttr("mux_live_stream.acc", "reconnect_window", "30"),
					resource.TestCheckNoResourceAttr("mux_live_stream.acc", "title"),
					resource.TestCheckNoResourceAttr("mux_live_stream.acc", "generated_subtitles"),
					resource.TestCheckResourceAttr("mux_live_stream.acc", "new_asset_settings.static_renditions.#", "1"),
				),
			},
			{Config: changed, ConfigPlanChecks: expectNoChanges},
		},
	})
}

// TestAccAPIFacts pins down, with direct API calls, the live behaviours the
// in-memory fake has to mirror, and logs what Mux answers. Everything it
// creates is deleted again.
func TestAccAPIFacts(t *testing.T) {
	testAccPreCheck(t)
	client, ctx := testAccClient(), context.Background()
	domain := "tf-acc-facts-" + randomSuffix() + ".example.com"
	outcome := func(what string, err error) {
		if err != nil {
			t.Logf("%s: status %d: %v", what, mux.StatusCode(err), err)
		} else {
			t.Logf("%s: accepted", what)
		}
	}
	expectStatus := func(what string, err error, want int) {
		t.Helper()
		outcome(what, err)
		if mux.StatusCode(err) != want {
			t.Errorf("%s: status %d, want %d", what, mux.StatusCode(err), want)
		}
	}

	// ---- playback restrictions
	restriction, err := client.CreatePlaybackRestriction(ctx,
		mux.ReferrerRestriction{AllowedDomains: []string{"z." + domain, "a." + domain}},
		mux.UserAgentRestriction{AllowNoUserAgent: true, AllowHighRiskUserAgent: true})
	if err != nil {
		t.Fatalf("create playback restriction: %v", err)
	}
	t.Cleanup(func() { _ = client.DeletePlaybackRestriction(ctx, restriction.ID) })
	if !slices.Equal(restriction.Referrer.AllowedDomains, []string{"z." + domain, "a." + domain}) {
		t.Errorf("domains are not kept in the order given: %v", restriction.Referrer.AllowedDomains)
	}
	emptied, err := client.UpdateReferrerRestriction(ctx, restriction.ID, mux.ReferrerRestriction{})
	if err != nil || emptied.ID != restriction.ID || len(emptied.Referrer.AllowedDomains) != 0 {
		t.Errorf("emptying the domains: %+v, %v", emptied, err)
	}
	_, err = client.UpdateReferrerRestriction(ctx, restriction.ID, mux.ReferrerRestriction{AllowedDomains: []string{"not a domain!"}})
	expectStatus("a malformed domain", err, 400)
	tooMany := make([]string, 101)
	for i := range tooMany {
		tooMany[i] = fmt.Sprintf("d%d.%s", i, domain)
	}
	_, err = client.UpdateReferrerRestriction(ctx, restriction.ID, mux.ReferrerRestriction{AllowedDomains: tooMany})
	expectStatus("101 domains", err, 400)
	all, err := client.ListPlaybackRestrictions(ctx)
	if err != nil || !slices.ContainsFunc(all, func(r mux.PlaybackRestriction) bool { return r.ID == restriction.ID }) {
		t.Errorf("the new restriction is not in the list of %d: %v", len(all), err)
	}
	if err := client.DeletePlaybackRestriction(ctx, restriction.ID); err != nil {
		t.Errorf("delete playback restriction: %v", err)
	}
	_, err = client.GetPlaybackRestriction(ctx, restriction.ID)
	expectStatus("reading a deleted playback restriction", err, 404)
	expectStatus("deleting it again", client.DeletePlaybackRestriction(ctx, restriction.ID), 404)
	_, err = client.GetPlaybackRestriction(ctx, "doesnotexist")
	expectStatus("reading an ID that could never exist", err, 400)

	// ---- webhooks
	webhook, err := client.CreateWebhook(ctx, "https://"+domain+"/hook")
	if err != nil {
		t.Fatalf("create webhook: %v", err)
	}
	t.Cleanup(func() { _ = client.DeleteWebhook(ctx, webhook.ID) })
	if !webhook.Enabled || webhook.SigningSecret == "" {
		t.Errorf("a new webhook is not enabled or has no signing secret")
	}
	if again, err := client.GetWebhook(ctx, webhook.ID); err != nil || again.SigningSecret != "" {
		t.Errorf("reading a webhook returns its signing secret, or fails: %v", err)
	}
	dup, err := client.CreateWebhook(ctx, "https://"+domain+"/hook")
	if err == nil {
		_ = client.DeleteWebhook(ctx, dup.ID)
	}
	expectStatus("a second webhook with the same address", err, 400)
	_, err = client.UpdateWebhook(ctx, webhook.ID, mux.WebhookUpdate{})
	expectStatus("a webhook update with no field", err, 400)
	stored, err := client.UpdateWebhook(ctx, webhook.ID, mux.WebhookUpdate{Address: ptr("tf-acc not a url " + domain)})
	outcome("a webhook address that is not a URL", err)
	if err == nil {
		t.Logf("  stored as %q", stored.Address)
	}

	// ---- live streams
	stream, err := client.CreateLiveStream(ctx, mux.LiveStreamCreate{})
	if err != nil {
		t.Fatalf("create live stream: %v", err)
	}
	t.Cleanup(func() { _ = client.DeleteLiveStream(ctx, stream.ID) })
	t.Logf("live stream defaults: latency %q, reconnect window %v, max continuous duration %d, new asset settings %+v",
		stream.LatencyMode, stream.ReconnectWindow, stream.MaxContinuousDuration, stream.NewAssetSettings)
	if stream.LatencyMode != "standard" || stream.ReconnectWindow != 60 || stream.MaxContinuousDuration != 43200 || !stream.NewAssetSettings.IsZero() {
		t.Errorf("unexpected live stream defaults")
	}
	low := "low"
	if _, err := client.UpdateLiveStream(ctx, stream.ID, mux.LiveStreamUpdate{LatencyMode: &low, Passthrough: ptr("tf-acc")}); err != nil {
		t.Fatalf("update live stream: %v", err)
	}
	_, err = client.SetGeneratedSubtitles(ctx, stream.ID, []mux.GeneratedSubtitles{{Name: "EN"}})
	expectStatus("generated subtitles on a low latency stream", err, 400)
	cleared, err := client.UpdateLiveStream(ctx, stream.ID, mux.LiveStreamUpdate{LatencyMode: ptr("standard"), Passthrough: ptr("")})
	if err != nil || cleared.Passthrough != "" {
		t.Errorf("an empty passthrough does not clear it: %+v, %v", cleared, err)
	}
	_, err = client.SetGeneratedSubtitles(ctx, stream.ID, []mux.GeneratedSubtitles{{Name: "EN"}, {Name: "ES", LanguageCode: "es"}})
	expectStatus("two generated subtitle tracks", err, 400)
	_, err = client.UpdateLiveStream(ctx, stream.ID, mux.LiveStreamUpdate{NewAssetSettings: &mux.NewAssetSettings{PlaybackPolicies: []string{"public"}}})
	expectStatus("changing the playback policies of new assets", err, 400)
	if err := client.SetStaticRenditions(ctx, stream.ID, []string{"highest"}); err != nil {
		t.Errorf("set static renditions: %v", err)
	}
	if err := client.SetStaticRenditions(ctx, stream.ID, nil); err != nil {
		t.Errorf("remove static renditions: %v", err)
	}
	playbackID, err := client.CreateLiveStreamPlaybackID(ctx, stream.ID, "signed", "")
	if err != nil {
		t.Fatalf("create playback ID: %v", err)
	}
	if err := client.DeleteLiveStreamPlaybackID(ctx, stream.ID, playbackID.ID); err != nil {
		t.Errorf("delete playback ID: %v", err)
	}
	_, err = client.GetLiveStreamPlaybackID(ctx, stream.ID, playbackID.ID)
	expectStatus("reading a deleted playback ID", err, 404)
	_, err = client.CreateLiveStreamPlaybackID(ctx, stream.ID, "drm", "")
	expectStatus("a drm playback ID without a DRM configuration", err, 400)
	if err := client.DeleteLiveStream(ctx, stream.ID); err != nil {
		t.Errorf("delete live stream: %v", err)
	}
	_, err = client.GetLiveStreamPlaybackID(ctx, stream.ID, playbackID.ID)
	expectStatus("reading a playback ID of a deleted live stream", err, 404)

	trial, err := client.CreateLiveStream(ctx, mux.LiveStreamCreate{Test: true, MaxContinuousDuration: ptr(int64(3600))})
	if err != nil {
		t.Fatalf("create test live stream: %v", err)
	}
	t.Cleanup(func() { _ = client.DeleteLiveStream(ctx, trial.ID) })
	if trial.MaxContinuousDuration != 300 {
		t.Errorf("a test stream's max continuous duration = %d, want Mux to fix it at 300", trial.MaxContinuousDuration)
	}
	_, err = client.UpdateLiveStream(ctx, trial.ID, mux.LiveStreamUpdate{MaxContinuousDuration: ptr(int64(300))})
	expectStatus("max continuous duration on a test stream", err, 400)
	_, err = client.CreateSimulcastTarget(ctx, trial.ID, mux.SimulcastTarget{URL: "rtmp://" + domain + "/app"})
	expectStatus("a simulcast target on a test stream", err, 400)

	// ---- transcription vocabularies
	vocabulary, err := client.CreateTranscriptionVocabulary(ctx, "tf-acc-facts", []string{"Mux"}, "tf-acc")
	if err != nil {
		t.Fatalf("create transcription vocabulary: %v", err)
	}
	t.Cleanup(func() { _ = client.DeleteTranscriptionVocabulary(ctx, vocabulary.ID) })
	replaced, err := client.UpdateTranscriptionVocabulary(ctx, vocabulary.ID, "", nil, "")
	if err != nil || replaced.Name != "" || replaced.Passthrough != "" || len(replaced.Phrases) != 0 {
		t.Errorf("an update does not replace the whole vocabulary: %+v, %v", replaced, err)
	}

	// ---- the rest
	drm, err := client.ListDRMConfigurations(ctx)
	if err != nil {
		t.Errorf("list DRM configurations: %v", err)
	}
	t.Logf("DRM configurations: %d", len(drm))
	key, err := client.CreateSigningKey(ctx)
	if err != nil {
		t.Fatalf("create signing key: %v", err)
	}
	t.Cleanup(func() { _ = client.DeleteSigningKey(ctx, key.ID) })
	if again, err := client.GetSigningKey(ctx, key.ID); err != nil || key.PrivateKey == "" || again.PrivateKey != "" {
		t.Errorf("the private key is not only in the create response: %v", err)
	}
	if err := client.DeleteSigningKey(ctx, key.ID); err != nil {
		t.Errorf("delete signing key: %v", err)
	}
	// The list drops a deleted key at once; a GET by ID may serve it from a
	// cache for minutes, which is why GetSigningKey reads the list.
	_, err = client.GetSigningKey(ctx, key.ID)
	expectStatus("reading a deleted signing key through the list", err, 404)
}
