// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/booldefault"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

var (
	_ resource.Resource                = (*webhookResource)(nil)
	_ resource.ResourceWithConfigure   = (*webhookResource)(nil)
	_ resource.ResourceWithImportState = (*webhookResource)(nil)
)

type webhookResource struct {
	data *providerData
}

func newWebhookResource() resource.Resource { return &webhookResource{} }

type webhookModel struct {
	ID            types.String `tfsdk:"id"`
	Address       types.String `tfsdk:"address"`
	Enabled       types.Bool   `tfsdk:"enabled"`
	SigningSecret types.String `tfsdk:"signing_secret"`
	CreatedAt     types.String `tfsdk:"created_at"`
}

// fill copies what Mux reports. The signing secret is only in the create
// response; otherwise the state's value stays.
func (m *webhookModel) fill(w *mux.Webhook) {
	m.ID = types.StringValue(w.ID)
	m.Address = types.StringValue(w.Address)
	m.Enabled = types.BoolValue(w.Enabled)
	m.CreatedAt = types.StringValue(w.CreatedAt)
	if w.SigningSecret != "" {
		m.SigningSecret = types.StringValue(w.SigningSecret)
	} else if m.SigningSecret.IsUnknown() {
		m.SigningSecret = types.StringNull()
	}
}

func (r *webhookResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_webhook"
}

func (r *webhookResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A webhook: an address Mux sends the environment's event notifications to. Changing " +
			"the address keeps the webhook and its signing secret.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Webhook ID.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"address": schema.StringAttribute{
				MarkdownDescription: "URL Mux sends notifications to. It must be unique among the environment's webhooks.",
				Required:            true,
				Validators: []validator.String{
					// Mux stores any string as the address.
					stringvalidator.RegexMatches(httpURLPattern, "must be an http:// or https:// URL"),
				},
			},
			"enabled": schema.BoolAttribute{
				MarkdownDescription: "Whether Mux delivers notifications to the webhook. Defaults to `true`.",
				Optional:            true,
				Computed:            true,
				Default:             booldefault.StaticBool(true),
			},
			"signing_secret": schema.StringAttribute{
				MarkdownDescription: "Secret for verifying that a notification was sent by Mux. Mux returns it only " +
					"when the webhook is created, so an imported webhook has none. It is stored in the state.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the webhook was created, as an ISO 8601 UTC datetime.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
		},
	}
}

func (r *webhookResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *webhookResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	enabled := plan.Enabled.ValueBool()
	// An address that is taken is refused by Mux, so an existing webhook is
	// never adopted.
	webhook, err := r.data.client.CreateWebhook(ctx, plan.Address.ValueString())
	if err != nil {
		resp.Diagnostics.AddError("Unable to create webhook", err.Error())
		return
	}
	plan.fill(webhook)
	// The webhook exists now: record it, secret included, before anything
	// else can fail.
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
	if resp.Diagnostics.HasError() || enabled {
		return
	}
	// A webhook starts out enabled; there is no way to create it disabled.
	webhook, err = r.data.client.UpdateWebhook(ctx, webhook.ID, mux.WebhookUpdate{Enabled: &enabled})
	if err != nil {
		resp.Diagnostics.AddError("Unable to disable the new webhook", err.Error())
		return
	}
	plan.fill(webhook)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	webhook, err := r.data.client.GetWebhook(ctx, state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read webhook", err.Error())
		return
	}
	state.fill(webhook)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *webhookResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan webhookModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	address, enabled := plan.Address.ValueString(), plan.Enabled.ValueBool()
	webhook, err := r.data.client.UpdateWebhook(ctx, plan.ID.ValueString(), mux.WebhookUpdate{Address: &address, Enabled: &enabled})
	if err != nil {
		resp.Diagnostics.AddError("Unable to update webhook", err.Error())
		return
	}
	plan.fill(webhook)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *webhookResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state webhookModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	if err := r.data.client.DeleteWebhook(ctx, state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete webhook", err.Error())
	}
}

// ImportState adopts an existing webhook by its ID. Its signing secret stays
// unknown to Terraform (null).
func (r *webhookResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
