// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/float64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/float64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringdefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

var (
	_ resource.Resource                     = (*liveStreamResource)(nil)
	_ resource.ResourceWithConfigure        = (*liveStreamResource)(nil)
	_ resource.ResourceWithImportState      = (*liveStreamResource)(nil)
	_ resource.ResourceWithConfigValidators = (*liveStreamResource)(nil)
)

type liveStreamResource struct {
	data *providerData
}

func newLiveStreamResource() resource.Resource { return &liveStreamResource{} }

type newAssetSettingsModel struct {
	PlaybackPolicies types.Set    `tfsdk:"playback_policies"`
	VideoQuality     types.String `tfsdk:"video_quality"`
	StaticRenditions types.Set    `tfsdk:"static_renditions"`
}

type embeddedSubtitlesModel struct {
	Name            types.String `tfsdk:"name"`
	LanguageCode    types.String `tfsdk:"language_code"`
	LanguageChannel types.String `tfsdk:"language_channel"`
	Passthrough     types.String `tfsdk:"passthrough"`
}

type generatedSubtitlesModel struct {
	Name                       types.String `tfsdk:"name"`
	LanguageCode               types.String `tfsdk:"language_code"`
	TranscriptionVocabularyIDs types.Set    `tfsdk:"transcription_vocabulary_ids"`
	Passthrough                types.String `tfsdk:"passthrough"`
}

type liveStreamModel struct {
	ID                         types.String              `tfsdk:"id"`
	Title                      types.String              `tfsdk:"title"`
	Passthrough                types.String              `tfsdk:"passthrough"`
	LatencyMode                types.String              `tfsdk:"latency_mode"`
	ReconnectWindow            types.Float64             `tfsdk:"reconnect_window"`
	MaxContinuousDuration      types.Int64               `tfsdk:"max_continuous_duration"`
	UseSlateForStandardLatency types.Bool                `tfsdk:"use_slate_for_standard_latency"`
	ReconnectSlateURL          types.String              `tfsdk:"reconnect_slate_url"`
	AudioOnly                  types.Bool                `tfsdk:"audio_only"`
	Test                       types.Bool                `tfsdk:"test"`
	NewAssetSettings           *newAssetSettingsModel    `tfsdk:"new_asset_settings"`
	EmbeddedSubtitles          []embeddedSubtitlesModel  `tfsdk:"embedded_subtitles"`
	GeneratedSubtitles         []generatedSubtitlesModel `tfsdk:"generated_subtitles"`
	StreamKey                  types.String              `tfsdk:"stream_key"`
	SRTPassphrase              types.String              `tfsdk:"srt_passphrase"`
	CreatedAt                  types.String              `tfsdk:"created_at"`
	DeletionProtection         types.Bool                `tfsdk:"deletion_protection"`
}

// fill copies what Mux reports into the model. The model's previous
// contents decide between null and empty where Mux reports nothing: Mux
// leaves out every unset setting, so an omitted argument and an empty one
// read back the same.
func (m *liveStreamModel) fill(ctx context.Context, l *mux.LiveStream) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(l.ID)
	m.Title = stringOrNull(l.Title())
	m.Passthrough = stringOrNull(l.Passthrough)
	m.LatencyMode = types.StringValue(l.LatencyMode)
	m.ReconnectWindow = types.Float64Value(l.ReconnectWindow)
	m.MaxContinuousDuration = types.Int64Value(l.MaxContinuousDuration)
	m.UseSlateForStandardLatency = types.BoolValue(l.UseSlateForStandardLatency)
	m.ReconnectSlateURL = stringOrNull(l.ReconnectSlateURL)
	m.AudioOnly = types.BoolValue(l.AudioOnly)
	m.Test = types.BoolValue(l.Test)
	m.StreamKey = types.StringValue(l.StreamKey)
	m.SRTPassphrase = stringOrNull(l.SRTPassphrase)
	m.CreatedAt = types.StringValue(l.CreatedAt)

	if l.NewAssetSettings.IsZero() && m.NewAssetSettings == nil {
		m.NewAssetSettings = nil
	} else {
		prior, api := m.NewAssetSettings, l.NewAssetSettings
		if prior == nil {
			prior = &newAssetSettingsModel{PlaybackPolicies: types.SetNull(types.StringType), StaticRenditions: types.SetNull(types.StringType)}
		}
		if api == nil {
			api = &mux.NewAssetSettings{}
		}
		resolutions := make([]string, 0, len(api.StaticRenditions))
		for _, r := range api.StaticRenditions {
			resolutions = append(resolutions, r.Resolution)
		}
		m.NewAssetSettings = &newAssetSettingsModel{
			PlaybackPolicies: stringSet(ctx, api.PlaybackPolicies, prior.PlaybackPolicies, &diags),
			VideoQuality:     stringOrNull(api.VideoQuality),
			StaticRenditions: stringSet(ctx, resolutions, prior.StaticRenditions, &diags),
		}
	}

	embedded := make([]embeddedSubtitlesModel, 0, len(l.EmbeddedSubtitles))
	for _, s := range l.EmbeddedSubtitles {
		embedded = append(embedded, embeddedSubtitlesModel{
			Name:            types.StringValue(s.Name),
			LanguageCode:    types.StringValue(s.LanguageCode),
			LanguageChannel: types.StringValue(s.LanguageChannel),
			Passthrough:     stringOrNull(s.Passthrough),
		})
	}
	if len(embedded) > 0 || m.EmbeddedSubtitles != nil {
		m.EmbeddedSubtitles = embedded
	}

	generated := make([]generatedSubtitlesModel, 0, len(l.GeneratedSubtitles))
	for i, s := range l.GeneratedSubtitles {
		priorIDs := types.SetNull(types.StringType)
		if i < len(m.GeneratedSubtitles) {
			priorIDs = m.GeneratedSubtitles[i].TranscriptionVocabularyIDs
		}
		generated = append(generated, generatedSubtitlesModel{
			Name:                       types.StringValue(s.Name),
			LanguageCode:               types.StringValue(s.LanguageCode),
			TranscriptionVocabularyIDs: stringSet(ctx, s.TranscriptionVocabularyIDs, priorIDs, &diags),
			Passthrough:                stringOrNull(s.Passthrough),
		})
	}
	if len(generated) > 0 || m.GeneratedSubtitles != nil {
		m.GeneratedSubtitles = generated
	}
	return diags
}

func (m *liveStreamModel) embedded() []mux.EmbeddedSubtitles {
	out := make([]mux.EmbeddedSubtitles, 0, len(m.EmbeddedSubtitles))
	for _, s := range m.EmbeddedSubtitles {
		out = append(out, mux.EmbeddedSubtitles{
			Name:            s.Name.ValueString(),
			LanguageCode:    s.LanguageCode.ValueString(),
			LanguageChannel: s.LanguageChannel.ValueString(),
			Passthrough:     s.Passthrough.ValueString(),
		})
	}
	return out
}

func (m *liveStreamModel) generated(ctx context.Context, diags *diag.Diagnostics) []mux.GeneratedSubtitles {
	out := make([]mux.GeneratedSubtitles, 0, len(m.GeneratedSubtitles))
	for _, s := range m.GeneratedSubtitles {
		out = append(out, mux.GeneratedSubtitles{
			Name:                       s.Name.ValueString(),
			LanguageCode:               s.LanguageCode.ValueString(),
			TranscriptionVocabularyIDs: setStrings(ctx, s.TranscriptionVocabularyIDs, diags),
			Passthrough:                s.Passthrough.ValueString(),
		})
	}
	return out
}

// staticRenditions returns the configured resolutions, or nil.
func (m *liveStreamModel) staticRenditions(ctx context.Context, diags *diag.Diagnostics) []string {
	if m.NewAssetSettings == nil {
		return nil
	}
	return setStrings(ctx, m.NewAssetSettings.StaticRenditions, diags)
}

func (r *liveStreamResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_live_stream"
}

func (r *liveStreamResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A live stream: a long-lived ingest endpoint an encoder broadcasts to with the stream " +
			"key. Meant for streams that are part of the configuration, such as a channel that is always there; " +
			"streams an application creates per event belong to the application. Playback IDs and simulcast " +
			"targets are resources of their own (`mux_live_stream_playback_id`, `mux_live_stream_simulcast_target`).",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Live stream ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"title": schema.StringAttribute{
				MarkdownDescription: "Title of the live stream (`meta.title`), at most 512 characters. Players may " +
					"show it, so it must not hold anything sensitive.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 512)},
			},
			"passthrough": schema.StringAttribute{
				MarkdownDescription: "Arbitrary metadata, at most 255 characters. It is copied to the assets " +
					"recorded from the stream and included in webhooks.",
				Optional:   true,
				Validators: []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"latency_mode": schema.StringAttribute{
				MarkdownDescription: "Latency of the stream: `standard`, `reduced` or `low`. Defaults to `standard`. " +
					"Mux does not allow subtitles on a `low` latency stream.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
				Validators:    []validator.String{stringvalidator.OneOf("standard", "reduced", "low")},
			},
			"reconnect_window": schema.Float64Attribute{
				MarkdownDescription: "Seconds Mux waits for a disconnected encoder to come back before it ends the " +
					"recording, from 0 to 1800. Mux defaults it to 60.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Float64{float64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Float64{float64validator.Between(0, 1800)},
			},
			"max_continuous_duration": schema.Int64Attribute{
				MarkdownDescription: "Seconds a broadcast may last before Mux disconnects it, from 60 to 43200. Mux " +
					"defaults it to 43200 (12 hours), and to 300 for a `test` stream.",
				Optional:      true,
				Computed:      true,
				PlanModifiers: []planmodifier.Int64{int64planmodifier.UseStateForUnknown()},
				Validators:    []validator.Int64{int64validator.Between(60, 43200)},
			},
			"use_slate_for_standard_latency": schema.BoolAttribute{
				MarkdownDescription: "Whether to insert slate media while a `standard` latency stream waits for " +
					"its encoder to reconnect. Defaults to `false`.",
				Optional: true,
				Computed: true,
				Default:  booldefault.StaticBool(false),
			},
			"reconnect_slate_url": schema.StringAttribute{
				MarkdownDescription: "URL of an image to show as slate media instead of Mux's default.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.RegexMatches(httpURLPattern, "must be an http:// or https:// URL")},
			},
			"audio_only": schema.BoolAttribute{
				MarkdownDescription: "Whether Mux drops the video track and processes only audio. Defaults to " +
					"`false`. Mux ignores a change to it, so changing it replaces the live stream.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"test": schema.BoolAttribute{
				MarkdownDescription: "Whether this is a test live stream: free, watermarked and limited to five " +
					"minutes per broadcast. Defaults to `false`. Changing it replaces the live stream.",
				Optional:      true,
				Computed:      true,
				Default:       booldefault.StaticBool(false),
				PlanModifiers: []planmodifier.Bool{boolplanmodifier.RequiresReplace()},
			},
			"new_asset_settings": schema.SingleNestedAttribute{
				MarkdownDescription: "Settings of the assets Mux records from the stream.",
				Optional:            true,
				PlanModifiers: []planmodifier.Object{objectplanmodifier.RequiresReplaceIf(
					assetPlaybackPoliciesChanged,
					"Replaces the live stream when the playback policies of new assets change.",
					"Replaces the live stream when the playback policies of new assets change.",
				)},
				Attributes: map[string]schema.Attribute{
					"playback_policies": schema.SetAttribute{
						MarkdownDescription: "Playback policies of each recorded asset: `public`, `signed`. Mux " +
							"does not let them change, so changing them replaces the live stream.",
						Optional:    true,
						ElementType: types.StringType,
						Validators: []validator.Set{setvalidator.ValueStringsAre(
							stringvalidator.OneOf("public", "signed"),
						)},
					},
					"video_quality": schema.StringAttribute{
						MarkdownDescription: "Video quality of each recorded asset: `plus` or `premium`. When " +
							"unset, the environment's default applies.",
						Optional:      true,
						Computed:      true,
						PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
						Validators:    []validator.String{stringvalidator.OneOf("plus", "premium")},
					},
					"static_renditions": schema.SetAttribute{
						MarkdownDescription: "Resolutions of the static renditions (MP4 or M4A files) to make of " +
							"each recorded asset: `highest`, `audio-only`, `2160p`, `1440p`, `1080p`, `720p`, " +
							"`540p`, `480p`, `360p`, `270p`.",
						Optional:    true,
						ElementType: types.StringType,
						Validators: []validator.Set{setvalidator.ValueStringsAre(stringvalidator.OneOf(
							"highest", "audio-only", "2160p", "1440p", "1080p", "720p", "540p", "480p", "360p", "270p",
						))},
					},
				},
			},
			"embedded_subtitles": schema.ListNestedAttribute{
				MarkdownDescription: "Closed captions embedded in the incoming stream (CEA-608) to pass on.",
				Optional:            true,
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{
						MarkdownDescription: "Name of the caption track.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"language_code": schema.StringAttribute{
						MarkdownDescription: "Language of the captions, as a BCP 47 code. Defaults to `en`.",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString("en"),
					},
					"language_channel": schema.StringAttribute{
						MarkdownDescription: "CEA-608 channel to read: `cc1` to `cc4`. Defaults to `cc1`.",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString("cc1"),
						Validators:          []validator.String{stringvalidator.OneOf("cc1", "cc2", "cc3", "cc4")},
					},
					"passthrough": schema.StringAttribute{
						MarkdownDescription: "Arbitrary metadata for the track, at most 255 characters.",
						Optional:            true,
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
					},
				}},
			},
			"generated_subtitles": schema.ListNestedAttribute{
				MarkdownDescription: "Subtitles generated by speech recognition. Mux allows one track.",
				Optional:            true,
				Validators:          []validator.List{listvalidator.SizeAtMost(1)},
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"name": schema.StringAttribute{
						MarkdownDescription: "Name of the subtitle track.",
						Required:            true,
						Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
					},
					"language_code": schema.StringAttribute{
						MarkdownDescription: "Language of the audio: `en`, `en-US`, `es`, `fr`, `de`, `pt` or `it`. Defaults to `en`.",
						Optional:            true,
						Computed:            true,
						Default:             stringdefault.StaticString("en"),
						Validators:          []validator.String{stringvalidator.OneOf("en", "en-US", "es", "fr", "de", "pt", "it")},
					},
					"transcription_vocabulary_ids": schema.SetAttribute{
						MarkdownDescription: "IDs of the transcription vocabularies (`mux_transcription_vocabulary`) to use.",
						Optional:            true,
						ElementType:         types.StringType,
					},
					"passthrough": schema.StringAttribute{
						MarkdownDescription: "Arbitrary metadata for the track, at most 255 characters.",
						Optional:            true,
						Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
					},
				}},
			},
			"stream_key": schema.StringAttribute{
				MarkdownDescription: "Key an encoder broadcasts with over RTMP. Whoever has it can broadcast to " +
					"the stream. It is stored in the state.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"srt_passphrase": schema.StringAttribute{
				MarkdownDescription: "Passphrase for encrypting a broadcast over SRT. It is stored in the state.",
				Computed:            true,
				Sensitive:           true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the live stream was created, in seconds since the Unix epoch.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"deletion_protection": deletionProtectionAttribute("a new live stream gets a new ID, stream key and " +
				"playback IDs, so every encoder and player would have to be reconfigured."),
		},
	}
}

// assetPlaybackPoliciesChanged requires a replacement when the playback
// policies of new assets differ between state and plan, which includes
// adding or removing the whole block.
func assetPlaybackPoliciesChanged(_ context.Context, req planmodifier.ObjectRequest, resp *objectplanmodifier.RequiresReplaceIfFuncResponse) {
	policies := func(obj types.Object) (types.Set, bool) {
		if obj.IsUnknown() {
			return types.Set{}, false
		}
		if obj.IsNull() {
			return types.SetNull(types.StringType), true
		}
		set, ok := obj.Attributes()["playback_policies"].(types.Set)
		return set, ok && !set.IsUnknown()
	}
	before, okBefore := policies(req.StateValue)
	after, okAfter := policies(req.PlanValue)
	if !okBefore || !okAfter {
		return
	}
	// Null and empty are the same to Mux: no policies.
	resp.RequiresReplace = !before.Equal(after) && len(before.Elements())+len(after.Elements()) > 0
}

func (r *liveStreamResource) ConfigValidators(context.Context) []resource.ConfigValidator {
	return []resource.ConfigValidator{testStreamDurationValidator{}}
}

// testStreamDurationValidator refuses max_continuous_duration on a test
// stream. Mux fixes it at 300 there: it ignores the value on create and
// refuses it on update ("Cannot use max continuous duration with test live
// streams", observed 2026-11-16).
type testStreamDurationValidator struct{}

func (testStreamDurationValidator) Description(context.Context) string {
	return "max_continuous_duration cannot be set on a test live stream"
}

func (v testStreamDurationValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (testStreamDurationValidator) ValidateResource(ctx context.Context, req resource.ValidateConfigRequest, resp *resource.ValidateConfigResponse) {
	var test types.Bool
	var duration types.Int64
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("test"), &test)...)
	resp.Diagnostics.Append(req.Config.GetAttribute(ctx, path.Root("max_continuous_duration"), &duration)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if test.ValueBool() && !duration.IsNull() && !duration.IsUnknown() {
		resp.Diagnostics.AddAttributeError(path.Root("max_continuous_duration"), "Not available on a test live stream",
			"Mux limits a broadcast on a test live stream to 300 seconds and does not accept another value. "+
				"Remove `max_continuous_duration`, or `test`.")
	}
}

func (r *liveStreamResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *liveStreamResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan liveStreamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	in := mux.LiveStreamCreate{
		LatencyMode:                plan.LatencyMode.ValueString(),
		UseSlateForStandardLatency: plan.UseSlateForStandardLatency.ValueBool(),
		ReconnectSlateURL:          plan.ReconnectSlateURL.ValueString(),
		Passthrough:                plan.Passthrough.ValueString(),
		AudioOnly:                  plan.AudioOnly.ValueBool(),
		Test:                       plan.Test.ValueBool(),
		EmbeddedSubtitles:          plan.embedded(),
		GeneratedSubtitles:         plan.generated(ctx, &resp.Diagnostics),
	}
	if !plan.ReconnectWindow.IsNull() && !plan.ReconnectWindow.IsUnknown() {
		in.ReconnectWindow = plan.ReconnectWindow.ValueFloat64Pointer()
	}
	if !plan.MaxContinuousDuration.IsNull() && !plan.MaxContinuousDuration.IsUnknown() {
		in.MaxContinuousDuration = plan.MaxContinuousDuration.ValueInt64Pointer()
	}
	if title := plan.Title.ValueString(); title != "" {
		in.Meta = &mux.Meta{Title: title}
	}
	if s := plan.NewAssetSettings; s != nil {
		settings := &mux.NewAssetSettings{
			PlaybackPolicies: setStrings(ctx, s.PlaybackPolicies, &resp.Diagnostics),
			VideoQuality:     s.VideoQuality.ValueString(),
		}
		for _, resolution := range setStrings(ctx, s.StaticRenditions, &resp.Diagnostics) {
			settings.StaticRenditions = append(settings.StaticRenditions, mux.StaticRendition{Resolution: resolution})
		}
		if !settings.IsZero() {
			in.NewAssetSettings = settings
		}
	}
	if resp.Diagnostics.HasError() {
		return
	}
	stream, err := r.data.client.CreateLiveStream(ctx, in)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create live stream", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, stream)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *liveStreamResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state liveStreamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	stream, err := r.data.client.GetLiveStream(ctx, state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read live stream", err.Error())
		return
	}
	resp.Diagnostics.Append(state.fill(ctx, stream)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update applies the plan in up to four calls, since Mux spreads a live
// stream's settings over several endpoints. Subtitles are removed before the
// main update and added after it, because Mux refuses subtitles on a low
// latency stream and the latency may be changing in the same apply.
func (r *liveStreamResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan, state liveStreamModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	client, id := r.data.client, state.ID.ValueString()

	embedded, embeddedBefore := plan.embedded(), state.embedded()
	generated, generatedBefore := plan.generated(ctx, &resp.Diagnostics), state.generated(ctx, &resp.Diagnostics)
	renditions, renditionsBefore := plan.staticRenditions(ctx, &resp.Diagnostics), state.staticRenditions(ctx, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	embeddedChanged := !equalJSON(embedded, embeddedBefore)
	generatedChanged := !equalJSON(generated, generatedBefore)

	// fail records the error and, since earlier calls may have landed, what
	// Mux has now, so the next plan starts from the truth.
	fail := func(summary string, err error) {
		resp.Diagnostics.AddError(summary, err.Error())
		if current, getErr := client.GetLiveStream(ctx, id); getErr == nil {
			resp.Diagnostics.Append(state.fill(ctx, current)...)
			resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
		}
	}

	if embeddedChanged && len(embedded) == 0 {
		if _, err := client.SetEmbeddedSubtitles(ctx, id, nil); err != nil {
			fail("Unable to remove the embedded subtitles of the live stream", err)
			return
		}
	}
	if generatedChanged && len(generated) == 0 {
		if _, err := client.SetGeneratedSubtitles(ctx, id, nil); err != nil {
			fail("Unable to remove the generated subtitles of the live stream", err)
			return
		}
	}

	title := plan.Title.ValueString()
	upd := mux.LiveStreamUpdate{
		LatencyMode:                plan.LatencyMode.ValueStringPointer(),
		ReconnectWindow:            plan.ReconnectWindow.ValueFloat64Pointer(),
		MaxContinuousDuration:      plan.MaxContinuousDuration.ValueInt64Pointer(),
		UseSlateForStandardLatency: plan.UseSlateForStandardLatency.ValueBoolPointer(),
		// An empty string clears these three.
		ReconnectSlateURL: ptr(plan.ReconnectSlateURL.ValueString()),
		Passthrough:       ptr(plan.Passthrough.ValueString()),
	}
	if plan.Test.ValueBool() {
		// Mux refuses the field on a test stream, whatever the value.
		upd.MaxContinuousDuration = nil
	}
	if title != "" || !state.Title.IsNull() {
		upd.Meta = &mux.Meta{Title: title}
	}
	if s := plan.NewAssetSettings; s != nil && s.VideoQuality.ValueString() != "" {
		upd.NewAssetSettings = &mux.NewAssetSettings{VideoQuality: s.VideoQuality.ValueString()}
	}
	if _, err := client.UpdateLiveStream(ctx, id, upd); err != nil {
		fail("Unable to update live stream", err)
		return
	}

	if embeddedChanged && len(embedded) > 0 {
		if _, err := client.SetEmbeddedSubtitles(ctx, id, embedded); err != nil {
			fail("Unable to set the embedded subtitles of the live stream", err)
			return
		}
	}
	if generatedChanged && len(generated) > 0 {
		if _, err := client.SetGeneratedSubtitles(ctx, id, generated); err != nil {
			fail("Unable to set the generated subtitles of the live stream", err)
			return
		}
	}
	if !equalStringSets(renditions, renditionsBefore) {
		if err := client.SetStaticRenditions(ctx, id, renditions); err != nil {
			fail("Unable to set the static renditions of the live stream", err)
			return
		}
	}

	stream, err := client.GetLiveStream(ctx, id)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read live stream", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, stream)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *liveStreamResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state liveStreamModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || deletionProtected(state.DeletionProtection, "live stream", &resp.Diagnostics) {
		return
	}
	// The assets recorded from the stream are kept.
	if err := r.data.client.DeleteLiveStream(ctx, state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete live stream", err.Error())
	}
}

func (r *liveStreamResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
