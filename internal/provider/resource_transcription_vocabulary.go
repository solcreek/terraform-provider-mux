// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/setvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

var (
	_ resource.Resource                = (*transcriptionVocabularyResource)(nil)
	_ resource.ResourceWithConfigure   = (*transcriptionVocabularyResource)(nil)
	_ resource.ResourceWithImportState = (*transcriptionVocabularyResource)(nil)
)

type transcriptionVocabularyResource struct {
	data *providerData
}

func newTranscriptionVocabularyResource() resource.Resource {
	return &transcriptionVocabularyResource{}
}

type transcriptionVocabularyModel struct {
	ID          types.String `tfsdk:"id"`
	Name        types.String `tfsdk:"name"`
	Phrases     types.Set    `tfsdk:"phrases"`
	Passthrough types.String `tfsdk:"passthrough"`
	CreatedAt   types.String `tfsdk:"created_at"`
}

func (m *transcriptionVocabularyModel) fill(ctx context.Context, v *mux.TranscriptionVocabulary) diag.Diagnostics {
	var diags diag.Diagnostics
	m.ID = types.StringValue(v.ID)
	m.Name = stringOrNull(v.Name)
	m.Passthrough = stringOrNull(v.Passthrough)
	m.CreatedAt = types.StringValue(v.CreatedAt)
	// Required, so never null: Mux leaves an empty list out.
	phrases := v.Phrases
	if phrases == nil {
		phrases = []string{}
	}
	var d diag.Diagnostics
	m.Phrases, d = types.SetValueFrom(ctx, types.StringType, phrases)
	diags.Append(d...)
	return diags
}

func (r *transcriptionVocabularyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_transcription_vocabulary"
}

func (r *transcriptionVocabularyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A transcription vocabulary: words, phrases and proper names that speech recognition " +
			"should favor in the generated subtitles of the live streams that use it " +
			"(`mux_live_stream.generated_subtitles`). A change does not reach a stream that is active.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Transcription vocabulary ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Name of the vocabulary.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthAtLeast(1)},
			},
			"phrases": schema.SetAttribute{
				MarkdownDescription: "The phrases, at most 1000, each of 1 to 32 characters. The order does not matter.",
				Required:            true,
				ElementType:         types.StringType,
				Validators: []validator.Set{
					setvalidator.SizeAtMost(1000),
					setvalidator.ValueStringsAre(stringvalidator.LengthBetween(1, 32)),
				},
			},
			"passthrough": schema.StringAttribute{
				MarkdownDescription: "Arbitrary metadata, at most 255 characters.",
				Optional:            true,
				Validators:          []validator.String{stringvalidator.LengthBetween(1, 255)},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the vocabulary was created, in seconds since the Unix epoch.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *transcriptionVocabularyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *transcriptionVocabularyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan transcriptionVocabularyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	phrases := setStrings(ctx, plan.Phrases, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	vocabulary, err := r.data.client.CreateTranscriptionVocabulary(ctx, plan.Name.ValueString(), phrases, plan.Passthrough.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create transcription vocabulary", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, vocabulary)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *transcriptionVocabularyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state transcriptionVocabularyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	vocabulary, err := r.data.client.GetTranscriptionVocabulary(ctx, state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read transcription vocabulary", err.Error())
		return
	}
	resp.Diagnostics.Append(state.fill(ctx, vocabulary)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *transcriptionVocabularyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan transcriptionVocabularyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	phrases := setStrings(ctx, plan.Phrases, &resp.Diagnostics)
	if resp.Diagnostics.HasError() {
		return
	}
	// The update replaces the vocabulary, so an unset name or passthrough is
	// removed.
	vocabulary, err := r.data.client.UpdateTranscriptionVocabulary(ctx, plan.ID.ValueString(), plan.Name.ValueString(), phrases, plan.Passthrough.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to update transcription vocabulary", err.Error())
		return
	}
	resp.Diagnostics.Append(plan.fill(ctx, vocabulary)...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *transcriptionVocabularyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state transcriptionVocabularyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	// Mux detaches the vocabulary from the live streams that use it.
	if err := r.data.client.DeleteTranscriptionVocabulary(ctx, state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete transcription vocabulary", err.Error())
	}
}

func (r *transcriptionVocabularyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
