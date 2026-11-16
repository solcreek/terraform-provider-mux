// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

var (
	_ resource.Resource                = (*playbackRestrictionResource)(nil)
	_ resource.ResourceWithConfigure   = (*playbackRestrictionResource)(nil)
	_ resource.ResourceWithImportState = (*playbackRestrictionResource)(nil)
)

// maxAllowedDomains is Mux's limit on a restriction's referrer domains
// (observed 2026-11-16: "has 101 entries (limit 100)").
const maxAllowedDomains = 100

type playbackRestrictionResource struct {
	data *providerData
}

func newPlaybackRestrictionResource() resource.Resource { return &playbackRestrictionResource{} }

type referrerModel struct {
	AllowedDomains  types.Set  `tfsdk:"allowed_domains"`
	AllowNoReferrer types.Bool `tfsdk:"allow_no_referrer"`
}

type userAgentModel struct {
	AllowNoUserAgent       types.Bool `tfsdk:"allow_no_user_agent"`
	AllowHighRiskUserAgent types.Bool `tfsdk:"allow_high_risk_user_agent"`
}

var userAgentAttrTypes = map[string]attr.Type{
	"allow_no_user_agent":        types.BoolType,
	"allow_high_risk_user_agent": types.BoolType,
}

type playbackRestrictionModel struct {
	ID                 types.String    `tfsdk:"id"`
	Referrer           *referrerModel  `tfsdk:"referrer"`
	UserAgent          *userAgentModel `tfsdk:"user_agent"`
	CreatedAt          types.String    `tfsdk:"created_at"`
	DeletionProtection types.Bool      `tfsdk:"deletion_protection"`
}

func (m *playbackRestrictionModel) fill(ctx context.Context, r *mux.PlaybackRestriction) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(r.ID)
	m.CreatedAt = types.StringValue(r.CreatedAt)
	m.Referrer, m.UserAgent = restrictionRules(ctx, r, &diags)
	return diags
}

// restrictionRules maps a restriction's rules onto the models. The domains
// are always a set, never null: an empty one denies every domain.
func restrictionRules(ctx context.Context, r *mux.PlaybackRestriction, diags *diag.Diagnostics) (*referrerModel, *userAgentModel) {
	domains := r.Referrer.AllowedDomains
	if domains == nil {
		domains = []string{}
	}
	set, d := types.SetValueFrom(ctx, types.StringType, domains)
	diags.Append(d...)
	return &referrerModel{AllowedDomains: set, AllowNoReferrer: types.BoolValue(r.Referrer.AllowNoReferrer)},
		&userAgentModel{
			AllowNoUserAgent:       types.BoolValue(r.UserAgent.AllowNoUserAgent),
			AllowHighRiskUserAgent: types.BoolValue(r.UserAgent.AllowHighRiskUserAgent),
		}
}

func (m *playbackRestrictionModel) referrer(ctx context.Context, diags *diag.Diagnostics) mux.ReferrerRestriction {
	return mux.ReferrerRestriction{
		AllowedDomains:  setStrings(ctx, m.Referrer.AllowedDomains, diags),
		AllowNoReferrer: m.Referrer.AllowNoReferrer.ValueBool(),
	}
}

func (m *playbackRestrictionModel) userAgent() mux.UserAgentRestriction {
	return mux.UserAgentRestriction{
		AllowNoUserAgent:       m.UserAgent.AllowNoUserAgent.ValueBool(),
		AllowHighRiskUserAgent: m.UserAgent.AllowHighRiskUserAgent.ValueBool(),
	}
}

func (r *playbackRestrictionResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_playback_restriction"
}

func (r *playbackRestrictionResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A playback restriction: the referrer domains and user agents allowed to play videos " +
			"whose signed playback token names the restriction's ID. Every change is made in place, so the ID, " +
			"which tokens and applications refer to, never changes. Mux takes a few minutes to enforce a new " +
			"list of domains.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Playback restriction ID, the `playback_restriction_id` claim of a signed playback token.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"referrer": schema.SingleNestedAttribute{
				MarkdownDescription: "Which referring domains may play the videos.",
				Required:            true,
				Attributes: map[string]schema.Attribute{
					"allowed_domains": schema.SetAttribute{
						MarkdownDescription: "Domains allowed to play the videos, at most 100. An entry is a domain " +
							"(`example.com`), a wildcard for its subdomains (`*.example.com`) or `*` for any domain. " +
							"An empty set denies every domain. The order does not matter.",
						Required:    true,
						ElementType: types.StringType,
						Validators: []validator.Set{
							setvalidator.SizeAtMost(maxAllowedDomains),
							setvalidator.ValueStringsAre(stringvalidator.LengthAtLeast(1)),
						},
					},
					"allow_no_referrer": schema.BoolAttribute{
						MarkdownDescription: "Whether to allow requests without a `Referer` header, which is what native " +
							"applications and smart TVs send. Defaults to `false`.",
						Optional: true,
						Computed: true,
						Default:  booldefault.StaticBool(false),
					},
				},
			},
			"user_agent": schema.SingleNestedAttribute{
				MarkdownDescription: "Which user agents may play the videos. Defaults to allowing all of them.",
				Optional:            true,
				Computed:            true,
				Default: objectdefault.StaticValue(types.ObjectValueMust(userAgentAttrTypes, map[string]attr.Value{
					"allow_no_user_agent":        types.BoolValue(true),
					"allow_high_risk_user_agent": types.BoolValue(true),
				})),
				Attributes: map[string]schema.Attribute{
					"allow_no_user_agent": schema.BoolAttribute{
						MarkdownDescription: "Whether to allow requests without a `User-Agent` header. Defaults to `true`.",
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(true),
					},
					"allow_high_risk_user_agent": schema.BoolAttribute{
						MarkdownDescription: "Whether to allow user agents Mux considers high risk. Defaults to `true`.",
						Optional:            true,
						Computed:            true,
						Default:             booldefault.StaticBool(true),
					},
				},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the restriction was created, in seconds since the Unix epoch.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"deletion_protection": deletionProtectionAttribute("deleting a restriction stops playback for every " +
				"token that names it, and a new restriction gets a new ID."),
		},
	}
}

func (r *playbackRestrictionResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *playbackRestrictionResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan playbackRestrictionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	referrer := plan.referrer(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	restriction, err := r.data.client.CreatePlaybackRestriction(ctx, referrer, plan.userAgent())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create playback restriction", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, restriction)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *playbackRestrictionResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state playbackRestrictionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	restriction, err := r.data.client.GetPlaybackRestriction(ctx, state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read playback restriction", err.Error())
		return
	}
	resp.Diagnostics.Append(state.fill(ctx, restriction)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update changes the rules through the two in-place endpoints, and only the
// one that differs, so the restriction keeps its ID.
func (r *playbackRestrictionResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state playbackRestrictionModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	id := state.ID.ValueString()
	var restriction *mux.PlaybackRestriction
	var err error
	if state.Referrer == nil || !plan.Referrer.AllowedDomains.Equal(state.Referrer.AllowedDomains) ||
		!plan.Referrer.AllowNoReferrer.Equal(state.Referrer.AllowNoReferrer) {
		referrer := plan.referrer(ctx, &resp.Diagnostics)
		if resp.Diagnostics.HasError() {
			return
		}
		if restriction, err = r.data.client.UpdateReferrerRestriction(ctx, id, referrer); err != nil {
			resp.Diagnostics.AddError("Unable to update the referrer rules of the playback restriction", err.Error())
			return
		}
	}
	if state.UserAgent == nil || *plan.UserAgent != *state.UserAgent {
		if restriction, err = r.data.client.UpdateUserAgentRestriction(ctx, id, plan.userAgent()); err != nil {
			resp.Diagnostics.AddError("Unable to update the user agent rules of the playback restriction", err.Error())
			// The referrer rules may have changed already; record what Mux has.
			if current, getErr := r.data.client.GetPlaybackRestriction(ctx, id); getErr == nil {
				resp.Diagnostics.Append(state.fill(ctx, current)...)
				resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
			}
			return
		}
	}
	if restriction == nil {
		// Only deletion_protection changed, which lives in the state.
		if restriction, err = r.data.client.GetPlaybackRestriction(ctx, id); err != nil {
			resp.Diagnostics.AddError("Unable to read playback restriction", err.Error())
			return
		}
	}
	resp.Diagnostics.Append(plan.fill(ctx, restriction)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *playbackRestrictionResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state playbackRestrictionModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || deletionProtected(state.DeletionProtection, "playback restriction", &resp.Diagnostics) {
		return
	}
	if err := r.data.client.DeletePlaybackRestriction(ctx, state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete playback restriction", err.Error())
	}
}

// ImportState adopts an existing restriction by its ID.
func (r *playbackRestrictionResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
