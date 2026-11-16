// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"path/filepath"
	"regexp"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

// TestMain serves the in-process provider under the solcreek namespace, which
// the test configurations name in required_providers, so the same tests run
// under Terraform and OpenTofu (TF_ACC_TERRAFORM_PATH=$(command -v tofu)).
func TestMain(m *testing.M) {
	if os.Getenv("TF_ACC_PROVIDER_NAMESPACE") == "" {
		_ = os.Setenv("TF_ACC_PROVIDER_NAMESPACE", "solcreek")
	}
	if os.Getenv("TF_ACC_PROVIDER_HOST") == "" && filepath.Base(os.Getenv("TF_ACC_TERRAFORM_PATH")) == "tofu" {
		_ = os.Setenv("TF_ACC_PROVIDER_HOST", "registry.opentofu.org")
	}
	os.Exit(m.Run())
}

// requiredProviders pins the provider source in every test configuration.
const requiredProviders = `
terraform {
  required_providers {
    mux = {
      source = "solcreek/mux"
    }
  }
}
`

// testProtoV6ProviderFactories wires the in-process provider for tests.
var testProtoV6ProviderFactories = map[string]func() (tfprotov6.ProviderServer, error){
	"mux": providerserver.NewProtocol6WithError(New("test")()),
}

// testAccPreCheck verifies the environment for acceptance tests, which make
// real API calls and require:

// testAccPreCheck verifies the environment for acceptance tests, which make
// real API calls and require:
//   - TF_ACC=1
//   - MUX_TOKEN_ID and MUX_TOKEN_SECRET  an access token with Mux Video and
//     System write permissions
//
// The token must belong to a development environment the tests may write
// to; they refuse any other type. Without TF_ACC the test is skipped.
func testAccPreCheck(t *testing.T) {
	t.Helper()
	if os.Getenv("TF_ACC") == "" {
		t.Skip("acceptance tests are skipped unless TF_ACC is set")
	}
	if os.Getenv("MUX_TOKEN_ID") == "" || os.Getenv("MUX_TOKEN_SECRET") == "" {
		t.Fatal("MUX_TOKEN_ID and MUX_TOKEN_SECRET must be set for acceptance tests")
	}
	env, err := testAccClient().WhoAmI(context.Background())
	if err != nil {
		t.Fatalf("whoami: %v", err)
	}
	if env.EnvironmentType != "development" {
		t.Fatalf("acceptance tests only run against a development environment, and this token belongs to a %q one", env.EnvironmentType)
	}
}

func testAccClient() *mux.Client {
	return mux.New(os.Getenv("MUX_TOKEN_ID"), os.Getenv("MUX_TOKEN_SECRET"), mux.WithBaseURL(os.Getenv("MUX_BASE_URL")))
}

func TestProviderMissingCredentials(t *testing.T) {
	newFakeMux(t) // clears MUX_* from the environment
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config:      requiredProviders + `data "mux_environment" "this" {}`,
			ExpectError: regexp.MustCompile(`Missing Mux credentials`),
		}},
	})
}

// An ID from the configuration is never paired with a secret from the
// environment, which may belong to another Mux environment.
func TestProviderIncompleteCredentials(t *testing.T) {
	f := newFakeMux(t)
	t.Setenv("MUX_TOKEN_ID", fakeTokenID)
	t.Setenv("MUX_TOKEN_SECRET", fakeTokenSecret)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: requiredProviders + `
provider "mux" {
  token_id = "` + fakeTokenID + `"
  base_url = "` + f.srv.URL + `"
}
data "mux_environment" "this" {}
`,
			ExpectError: regexp.MustCompile(`(?s)Incomplete Mux credentials.*token_secret`),
		}},
	})
}

func TestProviderInvalidToken(t *testing.T) {
	f := newFakeMux(t)
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: requiredProviders + `
provider "mux" {
  token_id     = "` + fakeTokenID + `"
  token_secret = "wrong"
  base_url     = "` + f.srv.URL + `"
}
data "mux_environment" "this" {}
`,
			ExpectError: regexp.MustCompile(`(?s)Unable to read environment.*missing,\s+wrong\s+or\s+revoked`),
		}},
	})
}

func TestProviderEnvironment(t *testing.T) {
	f := newFakeMux(t)
	t.Setenv("MUX_TOKEN_ID", fakeTokenID)
	t.Setenv("MUX_TOKEN_SECRET", fakeTokenSecret)
	t.Setenv("MUX_BASE_URL", f.srv.URL+"/") // a trailing slash is tolerated
	resource.UnitTest(t, resource.TestCase{
		ProtoV6ProviderFactories: testProtoV6ProviderFactories,
		Steps: []resource.TestStep{{
			Config: requiredProviders + `data "mux_environment" "this" {}`,
			Check: resource.ComposeAggregateTestCheckFunc(
				resource.TestCheckResourceAttr("data.mux_environment.this", "id", "env1"),
				resource.TestCheckResourceAttr("data.mux_environment.this", "type", "development"),
				resource.TestCheckResourceAttr("data.mux_environment.this", "organization_name", "Acme Inc"),
				resource.TestCheckTypeSetElemAttr("data.mux_environment.this", "permissions.*", "video:write"),
			),
		}},
	})
}

// randomSuffix returns a short random hex string for acceptance test names.
func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}
