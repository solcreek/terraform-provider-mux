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

func expectAction(name string, action plancheck.ResourceActionType) resource.ConfigPlanChecks {
	return resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, action)}}
}

var expectNoChanges = resource.ConfigPlanChecks{PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()}}

func remember(dst *string) func(string) error {
	return func(v string) error { *dst = v; return nil }
}

func unchanged(want *string) func(string) error {
	return func(v string) error {
		if v != *want {
			return fmt.Errorf("value changed from %s to %s", *want, v)
		}
		return nil
	}
}

func TestSigningKeyLifecycle(t *testing.T) {
	f := newFakeMux(t)
	const name = "mux_signing_key.playback"
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_signing_key\" \"playback\" {" + body + "}\n"
	}
	var id, privateKey string
	keyCount := func() int {
		f.mu.Lock()
		defer f.mu.Unlock()
		return len(f.keys)
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if keyCount() != 0 {
				return fmt.Errorf("signing key %s still exists", id)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(""),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", remember(&id)),
					resource.TestCheckResourceAttrWith(name, "private_key", remember(&privateKey)),
					resource.TestCheckResourceAttr(name, "deletion_protection", "true"),
				),
			},
			{
				// Mux never returns the private key again; the state keeps it.
				Config:           config(""),
				ConfigPlanChecks: expectNoChanges,
				Check:            resource.TestCheckResourceAttrWith(name, "private_key", unchanged(&privateKey)),
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"private_key", "deletion_protection"},
			},
			{
				Config:      config(""),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`Deletion protection is enabled`),
			},
			{
				Config:           config(" deletion_protection = false "),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", unchanged(&id)),
					resource.TestCheckResourceAttrWith(name, "private_key", unchanged(&privateKey)),
				),
			},
			{
				// Deleted in the dashboard: a new key, with a new private key.
				// Mux still answers a GET of the old ID, which must not hide it.
				PreConfig:        func() { f.mutate(func() { f.keys, f.deletedKeys = nil, []string{id} }) },
				Config:           config(" deletion_protection = false "),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionCreate),
				Check: resource.TestCheckResourceAttrWith(name, "private_key", func(v string) error {
					if v == privateKey || v == "" {
						return fmt.Errorf("the recreated key has no private key of its own")
					}
					return nil
				}),
			},
		},
	})
}

func TestWebhookLifecycle(t *testing.T) {
	f := newFakeMux(t)
	const name = "mux_webhook.events"
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_webhook\" \"events\" {\n" + body + "\n}\n"
	}
	var id, secret string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if snapshot(f, f.webhook, id) != nil {
				return fmt.Errorf("webhook %s still exists", id)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(`address = "https://example.com/mux"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", remember(&id)),
					resource.TestCheckResourceAttrWith(name, "signing_secret", remember(&secret)),
					resource.TestCheckResourceAttr(name, "enabled", "true"),
				),
			},
			{
				Config:           config(`address = "https://example.com/mux"`),
				ConfigPlanChecks: expectNoChanges,
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"signing_secret"},
			},
			{
				// A new address keeps the webhook and its secret.
				Config:           config("address = \"https://example.com/hooks/mux\"\n  enabled = false"),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", unchanged(&id)),
					resource.TestCheckResourceAttrWith(name, "signing_secret", unchanged(&secret)),
					func(*terraform.State) error {
						if w := snapshot(f, f.webhook, id); w.Enabled || w.Address != "https://example.com/hooks/mux" {
							return fmt.Errorf("webhook = %+v", w)
						}
						return nil
					},
				),
			},
			{
				// Switched back on in the dashboard: drift, switched off again.
				PreConfig:        func() { f.mutate(func() { f.webhook(id).Enabled = true }) },
				Config:           config("address = \"https://example.com/hooks/mux\"\n  enabled = false"),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: func(*terraform.State) error {
					if snapshot(f, f.webhook, id).Enabled {
						return fmt.Errorf("the webhook is still enabled")
					}
					return nil
				},
			},
		},
	})
}

func TestWebhookErrors(t *testing.T) {
	f := newFakeMux(t)
	f.mutate(func() {
		f.webhooks = append(f.webhooks, &fakeWebhook{ID: "taken1", Address: "https://taken.example.com/", Enabled: true})
	})
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_webhook\" \"x\" {\n" + body + "\n}\n"
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Mux would store this as it is.
				Config:      config(`address = "not a url"`),
				ExpectError: regexp.MustCompile(`must be an http:// or https:// URL`),
			},
			{
				// An existing webhook is never adopted.
				Config:      config(`address = "https://taken.example.com/"`),
				ExpectError: regexp.MustCompile(`(?s)Unable to create webhook.*already exists`),
			},
			{
				// Created disabled: Mux has no such create, so it is two calls.
				Config: config("address = \"https://example.com/a\"\n  enabled = false"),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("mux_webhook.x", "enabled", "false"),
					resource.TestCheckResourceAttrSet("mux_webhook.x", "signing_secret"),
				),
			},
			{
				Config:      config("address = \"https://taken.example.com/\"\n  enabled = false"),
				ExpectError: regexp.MustCompile(`(?s)Unable to update webhook.*already exists`),
			},
		},
	})
}

func TestTranscriptionVocabularyLifecycle(t *testing.T) {
	f := newFakeMux(t)
	const name = "mux_transcription_vocabulary.terms"
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_transcription_vocabulary\" \"terms\" {\n" + body + "\n}\n"
	}
	var id string
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if snapshot(f, f.vocabulary, id) != nil {
				return fmt.Errorf("vocabulary %s still exists", id)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config("name = \"Product names\"\n  phrases = [\"Mux\", \"OpenTofu\"]\n  passthrough = \"managed\""),
				Check:  resource.TestCheckResourceAttrWith(name, "id", remember(&id)),
			},
			{
				Config:           config("name = \"Product names\"\n  phrases = [\"OpenTofu\", \"Mux\"]\n  passthrough = \"managed\""),
				ConfigPlanChecks: expectNoChanges,
			},
			{ResourceName: name, ImportState: true, ImportStateVerify: true},
			{
				// The update replaces the vocabulary, so the name and the
				// passthrough that are no longer configured are removed.
				Config:           config(`phrases = ["Mux", "OpenTofu", "Terraform"]`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", unchanged(&id)),
					resource.TestCheckNoResourceAttr(name, "name"),
					func(*terraform.State) error {
						v := snapshot(f, f.vocabulary, id)
						if v.Name != "" || v.Passthrough != "" || len(v.Phrases) != 3 {
							return fmt.Errorf("vocabulary = %+v", v)
						}
						return nil
					},
				),
			},
			{
				Config:      config(`phrases = ["this phrase is far longer than thirty-two characters"]`),
				ExpectError: regexp.MustCompile(`between 1 and 32`),
			},
			{
				// A phrase removed in the dashboard is drift.
				PreConfig:        func() { f.mutate(func() { f.vocabulary(id).Phrases = []string{"Mux"} }) },
				Config:           config(`phrases = ["Mux", "OpenTofu", "Terraform"]`),
				ConfigPlanChecks: expectAction(name, plancheck.ResourceActionUpdate),
			},
			{
				// No phrases: Mux then reports no list at all.
				Config: config(`phrases = []`),
				Check:  resource.TestCheckResourceAttr(name, "phrases.#", "0"),
			},
			{
				Config:           config(`phrases = []`),
				ConfigPlanChecks: expectNoChanges,
			},
		},
	})
}

func TestDRMConfigurations(t *testing.T) {
	f := newFakeMux(t)
	config := f.providerConfig() + `data "mux_drm_configurations" "all" {}`
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				// Without DRM, Mux answers {"data": {}}.
				Config: config,
				Check:  resource.TestCheckResourceAttr("data.mux_drm_configurations.all", "ids.#", "0"),
			},
			{
				PreConfig: func() { f.mutate(func() { f.drmConfigs = slices.Repeat([]string{"drm1"}, 120) }) },
				Config:    config,
				// More than one page.
				Check: resource.TestCheckResourceAttr("data.mux_drm_configurations.all", "ids.#", "120"),
			},
		},
	})
}
