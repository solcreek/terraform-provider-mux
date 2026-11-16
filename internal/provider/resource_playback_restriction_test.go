// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"fmt"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
	"github.com/hashicorp/terraform-plugin-testing/plancheck"
	"github.com/hashicorp/terraform-plugin-testing/terraform"
)

func TestPlaybackRestrictionLifecycle(t *testing.T) {
	f := newFakeMux(t)
	config := func(body string) string {
		return f.providerConfig() + "resource \"mux_playback_restriction\" \"web\" {\n" + body + "\n}\n"
	}
	const name = "mux_playback_restriction.web"
	var id string
	sameID := resource.TestCheckResourceAttrWith(name, "id", func(v string) error {
		if v != id {
			return fmt.Errorf("id changed from %s to %s", id, v)
		}
		return nil
	})
	update := resource.ConfigPlanChecks{
		PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
	}
	domains := func(want ...string) error {
		got := slices.Clone(snapshot(f, f.restriction, id).Domains)
		slices.Sort(got)
		if !slices.Equal(got, want) {
			return fmt.Errorf("domains = %v, want %v", got, want)
		}
		return nil
	}
	two := `referrer = { allowed_domains = ["*.example.com", "example.org"] }`

	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		CheckDestroy: func(*terraform.State) error {
			if snapshot(f, f.restriction, id) != nil {
				return fmt.Errorf("playback restriction %s still exists", id)
			}
			return nil
		},
		Steps: []resource.TestStep{
			{
				Config: config(two),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttrWith(name, "id", func(v string) error { id = v; return nil }),
					resource.TestCheckResourceAttr(name, "referrer.allowed_domains.#", "2"),
					resource.TestCheckResourceAttr(name, "referrer.allow_no_referrer", "false"),
					resource.TestCheckResourceAttr(name, "user_agent.allow_no_user_agent", "true"),
					resource.TestCheckResourceAttr(name, "user_agent.allow_high_risk_user_agent", "true"),
					resource.TestCheckResourceAttr(name, "deletion_protection", "true"),
				),
			},
			{
				// The order of the domains is not a difference.
				Config: config(`referrer = { allowed_domains = ["example.org", "*.example.com"] }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				// Adding a domain goes through the referrer endpoint only, and
				// keeps the ID.
				PreConfig:        f.forgetRequests,
				Config:           config(`referrer = { allowed_domains = ["*.example.com", "example.org", "example.net"], allow_no_referrer = true }`),
				ConfigPlanChecks: update,
				Check: resource.ComposeAggregateTestCheckFunc(sameID, func(*terraform.State) error {
					if !f.sawRequest("PUT", "/video/v1/playback-restrictions/"+id+"/referrer") || f.sawRequest("PUT", "/video/v1/playback-restrictions/"+id+"/user_agent") {
						return fmt.Errorf("expected only the referrer endpoint to be called")
					}
					if f.sawRequest("POST", "") || f.sawRequest("DELETE", "") {
						return fmt.Errorf("the restriction was replaced")
					}
					if !snapshot(f, f.restriction, id).AllowNoReferrer {
						return fmt.Errorf("allow_no_referrer was not set")
					}
					return domains("*.example.com", "example.net", "example.org")
				}),
			},
			{
				// The user agent rules have an endpoint of their own.
				PreConfig: f.forgetRequests,
				Config: config(`referrer = { allowed_domains = ["*.example.com", "example.org", "example.net"], allow_no_referrer = true }
  user_agent = { allow_high_risk_user_agent = false }`),
				ConfigPlanChecks: update,
				Check: resource.ComposeAggregateTestCheckFunc(sameID,
					resource.TestCheckResourceAttr(name, "user_agent.allow_no_user_agent", "true"),
					func(*terraform.State) error {
						if f.sawRequest("PUT", "/video/v1/playback-restrictions/"+id+"/referrer") {
							return fmt.Errorf("the referrer rules were rewritten without a change")
						}
						if r := snapshot(f, f.restriction, id); r.AllowHighRisk || !r.AllowNoUA {
							return fmt.Errorf("user agent rules = %+v", r)
						}
						return nil
					}),
			},
			{
				// A domain added in the dashboard is drift, and is taken out again.
				PreConfig: func() {
					f.mutate(func() { f.restriction(id).Domains = append(f.restriction(id).Domains, "added-by-hand.example") })
				},
				Config:           config(two),
				ConfigPlanChecks: update,
				Check: resource.ComposeAggregateTestCheckFunc(sameID, func(*terraform.State) error {
					return domains("*.example.com", "example.org")
				}),
			},
			{
				// No domains at all denies every domain; Mux then reports no list.
				Config:           config(`referrer = { allowed_domains = [] }`),
				ConfigPlanChecks: update,
				Check:            resource.TestCheckResourceAttr(name, "referrer.allowed_domains.#", "0"),
			},
			{
				Config: config(`referrer = { allowed_domains = [] }`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectEmptyPlan()},
				},
			},
			{
				ResourceName:            name,
				ImportState:             true,
				ImportStateVerify:       true,
				ImportStateVerifyIgnore: []string{"deletion_protection"},
			},
			{
				// Protected: a destroy is refused and nothing is deleted.
				Config:      config(`referrer = { allowed_domains = [] }`),
				Destroy:     true,
				ExpectError: regexp.MustCompile(`Deletion protection is enabled`),
			},
			{
				Config:           config("referrer = { allowed_domains = [] }\n  deletion_protection = false"),
				ConfigPlanChecks: update,
				Check:            sameID,
			},
		},
	})
}

// An existing restriction is adopted by import and then changed in place:
// applications store its ID, so it must never be replaced.
func TestPlaybackRestrictionAdoption(t *testing.T) {
	f := newFakeMux(t)
	existing := f.addRestriction("*.example.com", "example.org")
	f.addRestriction("native.example.com")
	const name = "mux_playback_restriction.web"
	config := func(domains string) string {
		return f.providerConfig() + "resource \"mux_playback_restriction\" \"web\" {\n  referrer = { allowed_domains = [" + domains + "] }\n}\n" +
			"data \"mux_playback_restrictions\" \"all\" {}\n"
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:             config(`"*.example.com", "example.org"`),
				ResourceName:       name,
				ImportState:        true,
				ImportStateId:      existing.ID,
				ImportStatePersist: true,
			},
			{
				// The configuration matches what is there: all that is left to
				// record is the deletion protection, which no API call carries.
				PreConfig: f.forgetRequests,
				Config:    config(`"*.example.com", "example.org"`),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr(name, "id", existing.ID),
					resource.TestCheckResourceAttr(name, "deletion_protection", "true"),
					resource.TestCheckResourceAttr("data.mux_playback_restrictions.all", "restrictions.#", "2"),
					resource.TestCheckResourceAttr("data.mux_playback_restrictions.all", "restrictions.0.id", existing.ID),
					resource.TestCheckResourceAttr("data.mux_playback_restrictions.all", "restrictions.0.referrer.allowed_domains.#", "2"),
					resource.TestCheckResourceAttr("data.mux_playback_restrictions.all", "restrictions.1.referrer.allow_no_referrer", "false"),
					func(*terraform.State) error {
						if f.sawRequest("PUT", "") || f.sawRequest("POST", "") || f.sawRequest("DELETE", "") {
							return fmt.Errorf("adopting an unchanged restriction wrote to the API")
						}
						return nil
					},
				),
			},
			{
				Config: config(`"*.example.com", "example.org", "new.example.com"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction(name, plancheck.ResourceActionUpdate)},
				},
				Check: resource.TestCheckResourceAttr(name, "id", existing.ID),
			},
			{
				// Removed from the configuration while protected: it stays.
				Config:      f.providerConfig(),
				ExpectError: regexp.MustCompile(`Deletion protection is enabled`),
			},
			{
				// Unprotecting is a change to the state only; the test's own
				// teardown then deletes the restriction.
				PreConfig: f.forgetRequests,
				Config: strings.Replace(config(`"*.example.com", "example.org", "new.example.com"`),
					"referrer =", "deletion_protection = false\n  referrer =", 1),
				Check: func(*terraform.State) error {
					if f.sawRequest("PUT", "") {
						return fmt.Errorf("changing deletion_protection rewrote the rules")
					}
					return nil
				},
			},
		},
	})
}

func TestPlaybackRestrictionErrors(t *testing.T) {
	f := newFakeMux(t)
	config := func(domains string) string {
		return f.providerConfig() + "resource \"mux_playback_restriction\" \"x\" {\n  referrer = { allowed_domains = [" + domains + "] }\n  deletion_protection = false\n}\n"
	}
	many := make([]string, 0, 101)
	for i := range 101 {
		many = append(many, fmt.Sprintf("%q", fmt.Sprintf("d%d.example.com", i)))
	}
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config:      config(strings.Join(many, ", ")),
				ExpectError: regexp.MustCompile(`at most 100`),
			},
			{
				// Mux's own reason is passed on.
				Config:      config(`"not a domain!"`),
				ExpectError: regexp.MustCompile(`(?s)Unable to create playback restriction.*Malformed referrer domain`),
			},
			{
				Config: config(`"example.com"`),
			},
			{
				Config:      config(`"example.com", "still not a domain!"`),
				ExpectError: regexp.MustCompile(`(?s)Unable to update the referrer rules.*Malformed referrer domain`),
			},
			{
				// Deleted in the dashboard: recreated.
				PreConfig: func() { f.mutate(func() { f.restrictions = nil }) },
				Config:    config(`"example.com"`),
				ConfigPlanChecks: resource.ConfigPlanChecks{
					PreApply: []plancheck.PlanCheck{plancheck.ExpectResourceAction("mux_playback_restriction.x", plancheck.ResourceActionCreate)},
				},
			},
		},
	})
}
