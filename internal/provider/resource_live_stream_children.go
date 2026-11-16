// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

// Playback IDs and simulcast targets belong to a live stream and have no
// update endpoint: every argument replaces them.

var (
	_ resource.Resource                     = (*liveStreamPlaybackIDResource)(nil)
	_ resource.ResourceWithConfigure        = (*liveStreamPlaybackIDResource)(nil)
	_ resource.ResourceWithImportState      = (*liveStreamPlaybackIDResource)(nil)
	_ resource.ResourceWithConfigValidators = (*liveStreamPlaybackIDResource)(nil)
	_ resource.Resource                     = (*liveStreamSimulcastTargetResource)(nil)
	_ resource.ResourceWithConfigure        = (*liveStreamSimulcastTargetResource)(nil)
	_ resource.ResourceWithImportState      = (*liveStreamSimulcastTargetResource)(nil)
)

var replaceString = []planmodifier.String{stringplanmodifier.RequiresReplace()}

// ------------------------------------------------ mux_live_stream_playback_id

type liveStreamPlaybackIDResource struct {
	data *providerData
}

func newLiveStreamPlaybackIDResource() resource.Resource { return &liveStreamPlaybackIDResource{} }

type liveStreamPlaybackIDModel struct {
	ID                 types.String `tfsdk:"id"`
	LiveStreamID       types.String `tfsdk:"live_stream_id"`
	Policy             types.String `tfsdk:"policy"`
	DRMConfigurationID types.String `tfsdk:"drm_configuration_id"`
}

func (m *liveStreamPlaybackIDModel) fill(p *mux.PlaybackID) {
	m.ID = types.StringValue(p.ID)
	m.Policy = types.StringValue(p.Policy)
	m.DRMConfigurationID = stringOrNull(p.DRMConfigurationID)
}

func (r *liveStreamPlaybackIDResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_live_stream_playback_id"
}

func (r *liveStreamPlaybackIDResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A playback ID of a live stream: what viewers play it through, at " +
			"`https://stream.mux.com/<id>.m3u8`. Changing any argument replaces the playback ID, which changes " +
			"its ID.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "The playback ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"live_stream_id": schema.StringAttribute{
				MarkdownDescription: "ID of the live stream, e.g. `mux_live_stream.example.id`.",
				Required:            true,
				PlanModifiers:       replaceString,
			},
			"policy": schema.StringAttribute{
				MarkdownDescription: "`public` (anyone with the URL can play), `signed` (playback needs a token " +
					"signed with a signing key) or `drm` (needs `drm_configuration_id`).",
				Required:      true,
				PlanModifiers: replaceString,
				Validators:    []validator.String{stringvalidator.OneOf(playbackPolicies...)},
			},
			"drm_configuration_id": schema.StringAttribute{
				MarkdownDescription: "ID of the DRM configuration, for the `drm` policy only. " +
					"`data.mux_drm_configurations` lists the environment's.",
				Optional:      true,
				PlanModifiers: replaceString,
			},
		},
	}
}

func (r *liveStreamPlaybackIDResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{drmConfigurationValidator{}}
}

// drmConfigurationValidator ties drm_configuration_id to the drm policy.
type drmConfigurationValidator struct{}

func (drmConfigurationValidator) Description(context.Context) string {
	return "drm_configuration_id is required with the drm policy and not allowed with the others"
}

func (v drmConfigurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (drmConfigurationValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var cfg liveStreamPlaybackIDModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &cfg)...)
	if resp.Diagnostics.HasError() || cfg.Policy.IsUnknown() || cfg.DRMConfigurationID.IsUnknown() {
		return
	}
	drm, hasConfiguration := cfg.Policy.ValueString() == "drm", !cfg.DRMConfigurationID.IsNull()
	switch {
	case drm && !hasConfiguration:
		resp.Diagnostics.AddAttributeError(path.Root("drm_configuration_id"), "Missing DRM configuration",
			"The `drm` policy needs `drm_configuration_id`.")
	case !drm && hasConfiguration:
		resp.Diagnostics.AddAttributeError(path.Root("drm_configuration_id"), "Unexpected DRM configuration",
			"`drm_configuration_id` only applies to the `drm` policy.")
	}
}

func (r *liveStreamPlaybackIDResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *liveStreamPlaybackIDResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan liveStreamPlaybackIDModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	playbackID, err := r.data.client.CreateLiveStreamPlaybackID(ctx, plan.LiveStreamID.ValueString(), plan.Policy.ValueString(), plan.DRMConfigurationID.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create playback ID", err.Error())
		return
	}
	plan.fill(playbackID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *liveStreamPlaybackIDResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state liveStreamPlaybackIDModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A 404 is the playback ID or its live stream being gone.
	playbackID, err := r.data.client.GetLiveStreamPlaybackID(ctx, state.LiveStreamID.ValueString(), state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read playback ID", err.Error())
		return
	}
	state.fill(playbackID)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every argument requires replacement.
func (r *liveStreamPlaybackIDResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *liveStreamPlaybackIDResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state liveStreamPlaybackIDModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.client.DeleteLiveStreamPlaybackID(ctx, state.LiveStreamID.ValueString(), state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete playback ID", err.Error())
	}
}

// ImportState takes "<live_stream_id>/<playback_id>".
func (r *liveStreamPlaybackIDResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importParentChild(ctx, req, resp, "live_stream_id")
}

// ------------------------------------------- mux_live_stream_simulcast_target

type liveStreamSimulcastTargetResource struct {
	data *providerData
}

func newLiveStreamSimulcastTargetResource() resource.Resource {
	return &liveStreamSimulcastTargetResource{}
}

type liveStreamSimulcastTargetModel struct {
	ID           types.String `tfsdk:"id"`
	LiveStreamID types.String `tfsdk:"live_stream_id"`
	URL          types.String `tfsdk:"url"`
	StreamKey    types.String `tfsdk:"stream_key"`
	Passthrough  types.String `tfsdk:"passthrough"`
}

func (m *liveStreamSimulcastTargetModel) fill(t *mux.SimulcastTarget) {
	m.ID = types.StringValue(t.ID)
	m.URL = types.StringValue(t.URL)
	m.StreamKey = stringOrNull(t.StreamKey)
	m.Passthrough = stringOrNull(t.Passthrough)
}

func (r *liveStreamSimulcastTargetResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_live_stream_simulcast_target"
}

func (r *liveStreamSimulcastTargetResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A simulcast target of a live stream: a third-party service Mux restreams the " +
			"broadcast to. Mux only adds or removes a target while the live stream is idle, so an apply during " +
			"a broadcast fails. Changing any argument replaces the target.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Simulcast target ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"live_stream_id": schema.StringAttribute{
				MarkdownDescription: "ID of the live stream, e.g. `mux_live_stream.example.id`.",
				Required:            true,
				PlanModifiers:       replaceString,
			},
			"url": schema.StringAttribute{
				MarkdownDescription: "RTMP(S) or SRT endpoint of the destination. For RTMP it includes the " +
					"application name, e.g. `rtmp://live.example.com/app`.",
				Required:      true,
				PlanModifiers: replaceString,
				Validators:    []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"stream_key": schema.StringAttribute{
				MarkdownDescription: "The destination's stream key, for RTMP(S) only. It is stored in the state.",
				Optional:            true,
				Sensitive:           true,
				PlanModifiers:       replaceString,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"passthrough": schema.StringAttribute{
				MarkdownDescription: "Arbitrary metadata for the target.",
				Optional:            true,
				PlanModifiers:       replaceString,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
		},
	}
}

func (r *liveStreamSimulcastTargetResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *liveStreamSimulcastTargetResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan liveStreamSimulcastTargetModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	target, err := r.data.client.CreateSimulcastTarget(ctx, plan.LiveStreamID.ValueString(), mux.SimulcastTarget{
		URL:         plan.URL.ValueString(),
		StreamKey:   plan.StreamKey.ValueString(),
		Passthrough: plan.Passthrough.ValueString(),
	})
	if err != nil {
		resp.Diagnostics.AddError("Unable to create simulcast target", err.Error())
		return
	}
	plan.fill(target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *liveStreamSimulcastTargetResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state liveStreamSimulcastTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// A 404 is the target or its live stream being gone.
	target, err := r.data.client.GetSimulcastTarget(ctx, state.LiveStreamID.ValueString(), state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read simulcast target", err.Error())
		return
	}
	state.fill(target)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update is never called: every argument requires replacement.
func (r *liveStreamSimulcastTargetResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resp.Diagnostics.Append(resp.State.Set(ctx, req.Plan.Raw)...)
}

func (r *liveStreamSimulcastTargetResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state liveStreamSimulcastTargetModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.client.DeleteSimulcastTarget(ctx, state.LiveStreamID.ValueString(), state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete simulcast target", err.Error())
	}
}

// ImportState takes "<live_stream_id>/<simulcast_target_id>".
func (r *liveStreamSimulcastTargetResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	importParentChild(ctx, req, resp, "live_stream_id")
}
