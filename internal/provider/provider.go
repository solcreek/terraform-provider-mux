// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

// Ensure the provider satisfies the framework interface.
var _ provider.Provider = (*muxProvider)(nil)

type muxProvider struct {
	version string
}

type providerModel struct {
	TokenID        types.String `tfsdk:"token_id"`
	TokenSecret    types.String `tfsdk:"token_secret"`
	BaseURL        types.String `tfsdk:"base_url"`
	TimeoutSeconds types.Int64  `tfsdk:"timeout_seconds"`
}

// providerData is handed to every resource and data source.
type providerData struct {
	client *mux.Client
}

// New returns a provider factory for the given version.
func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &muxProvider{version: version}
	}
}

func (p *muxProvider) Metadata(_ context.Context, _ provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "mux"
	resp.Version = p.version
}

func (p *muxProvider) Schema(_ context.Context, _ provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manage the configuration of one Mux environment through the Mux API: playback " +
			"restrictions, signing keys, webhooks, live streams with their playback IDs and simulcast targets, " +
			"and transcription vocabularies. Authenticates with a Mux access token, which belongs to one " +
			"environment; use a provider alias per environment.",
		Attributes: map[string]schema.Attribute{
			"token_id": schema.StringAttribute{
				MarkdownDescription: "ID of a Mux access token. Falls back to the `MUX_TOKEN_ID` environment variable.",
				Optional:            true,
			},
			"token_secret": schema.StringAttribute{
				MarkdownDescription: "Secret of the access token. Falls back to the `MUX_TOKEN_SECRET` environment variable.",
				Optional:            true,
				Sensitive:           true,
			},
			"base_url": schema.StringAttribute{
				MarkdownDescription: "Mux API base URL. Defaults to `https://api.mux.com`. Falls back to `MUX_BASE_URL`.",
				Optional:            true,
			},
			"timeout_seconds": schema.Int64Attribute{
				MarkdownDescription: "Per-request HTTP timeout in seconds. Defaults to 60.",
				Optional:            true,
			},
		},
	}
}

func (p *muxProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var cfg providerModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() {
		return
	}
	for attr, v := range map[string]types.String{"token_id": cfg.TokenID, "token_secret": cfg.TokenSecret, "base_url": cfg.BaseURL} {
		if v.IsUnknown() {
			resp.Diagnostics.AddAttributeError(path.Root(attr), "Unknown provider configuration",
				fmt.Sprintf("`%s` must be known when the provider is configured; it cannot depend on a value that is only known after apply.", attr))
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}

	// The ID and the secret are one credential: take both from the
	// configuration or both from the environment, never one of each, so an
	// aliased provider cannot pair its ID with another environment's secret.
	tokenID, tokenSecret := cfg.TokenID.ValueString(), cfg.TokenSecret.ValueString()
	switch {
	case tokenID == "" && tokenSecret == "":
		tokenID, tokenSecret = os.Getenv("MUX_TOKEN_ID"), os.Getenv("MUX_TOKEN_SECRET")
		if tokenID == "" || tokenSecret == "" {
			resp.Diagnostics.AddError(
				"Missing Mux credentials",
				"Set the provider `token_id` and `token_secret` arguments, or the MUX_TOKEN_ID and MUX_TOKEN_SECRET environment variables.",
			)
		}
	case tokenID == "":
		resp.Diagnostics.AddAttributeError(path.Root("token_id"), "Incomplete Mux credentials",
			"`token_secret` is set but `token_id` is not. Set both, or neither to use MUX_TOKEN_ID and MUX_TOKEN_SECRET.")
	case tokenSecret == "":
		resp.Diagnostics.AddAttributeError(path.Root("token_secret"), "Incomplete Mux credentials",
			"`token_id` is set but `token_secret` is not. Set both, or neither to use MUX_TOKEN_ID and MUX_TOKEN_SECRET.")
	}
	if resp.Diagnostics.HasError() {
		return
	}

	timeout := 60 * time.Second
	if !cfg.TimeoutSeconds.IsNull() && cfg.TimeoutSeconds.ValueInt64() > 0 {
		timeout = time.Duration(cfg.TimeoutSeconds.ValueInt64()) * time.Second
	}

	client := mux.New(tokenID, tokenSecret,
		mux.WithBaseURL(firstNonEmpty(cfg.BaseURL.ValueString(), os.Getenv("MUX_BASE_URL"))),
		mux.WithTimeout(timeout),
		mux.WithUserAgent("terraform-provider-mux/"+p.version),
	)

	data := &providerData{client: client}
	resp.ResourceData = data
	resp.DataSourceData = data
}

func (p *muxProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		newPlaybackRestrictionResource,
		newSigningKeyResource,
		newWebhookResource,
		newTranscriptionVocabularyResource,
		newLiveStreamResource,
		newLiveStreamPlaybackIDResource,
		newLiveStreamSimulcastTargetResource,
	}
}

func (p *muxProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{
		newEnvironmentDataSource,
		newPlaybackRestrictionsDataSource,
		newDRMConfigurationsDataSource,
	}
}

// configureData extracts the provider data in a resource or data source
// Configure method. It returns nil before the provider is configured.
func configureData(raw any, diags *diag.Diagnostics) *providerData {
	if raw == nil {
		return nil
	}
	d, ok := raw.(*providerData)
	if !ok {
		diags.AddError("Unexpected provider data", fmt.Sprintf("Expected *providerData, got %T. Please report this issue to the provider developers.", raw))
		return nil
	}
	return d
}

func firstNonEmpty(vals ...string) string {
	for _, v := range vals {
		if v != "" {
			return v
		}
	}
	return ""
}
