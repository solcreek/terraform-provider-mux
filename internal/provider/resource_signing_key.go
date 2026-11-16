// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/solcreek/terraform-provider-mux/internal/mux"
)

var (
	_ resource.Resource                = (*signingKeyResource)(nil)
	_ resource.ResourceWithConfigure   = (*signingKeyResource)(nil)
	_ resource.ResourceWithImportState = (*signingKeyResource)(nil)
)

type signingKeyResource struct {
	data *providerData
}

func newSigningKeyResource() resource.Resource { return &signingKeyResource{} }

type signingKeyModel struct {
	ID                 types.String `tfsdk:"id"`
	PrivateKey         types.String `tfsdk:"private_key"`
	CreatedAt          types.String `tfsdk:"created_at"`
	DeletionProtection types.Bool   `tfsdk:"deletion_protection"`
}

func (r *signingKeyResource) Metadata(_ context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_signing_key"
}

func (r *signingKeyResource) Schema(_ context.Context, _ resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "A signing key: an RSA key pair for signing playback and data tokens (JWTs). Mux " +
			"generates the pair, keeps the public key and returns the private key once, at creation.\n\n" +
			"~> The private key is stored in the state, in plain text. Use this resource only where the state " +
			"is encrypted and access to it is controlled; otherwise create the key outside Terraform.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Signing key ID, the `kid` header of a token signed with the key.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"private_key": schema.StringAttribute{
				MarkdownDescription: "The private key: a Base64-encoded PEM file, for the RS256 algorithm. " +
					"`base64decode()` gives the PEM. Mux returns it only when the key is created, so an imported " +
					"key has none.",
				Computed:      true,
				Sensitive:     true,
				PlanModifiers: []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"created_at": schema.StringAttribute{
				MarkdownDescription: "When the key was created, in seconds since the Unix epoch.",
				Computed:            true,
				PlanModifiers:       []planmodifier.String{stringplanmodifier.UseStateForUnknown()},
			},
			"deletion_protection": deletionProtectionAttribute("deleting a signing key invalidates every token " +
				"signed with it, and the key cannot be restored."),
		},
	}
}

func (r *signingKeyResource) Configure(_ context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	r.data = configureData(req.ProviderData, &resp.Diagnostics)
}

func (r *signingKeyResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	var plan signingKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.data.client.CreateSigningKey(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to create signing key", err.Error())
		return
	}
	plan.ID = types.StringValue(key.ID)
	plan.PrivateKey = stringOrNull(key.PrivateKey)
	plan.CreatedAt = types.StringValue(key.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *signingKeyResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state signingKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}
	key, err := r.data.client.GetSigningKey(ctx, state.ID.ValueString())
	if err != nil {
		if mux.IsNotFound(err) {
			resp.State.RemoveResource(ctx)
			return
		}
		resp.Diagnostics.AddError("Unable to read signing key", err.Error())
		return
	}
	// The private key is not readable; what the state has stays.
	state.ID = types.StringValue(key.ID)
	state.CreatedAt = types.StringValue(key.CreatedAt)
	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

// Update only ever changes deletion_protection, which lives in the state.
func (r *signingKeyResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	var plan signingKeyModel
	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	if resp.Diagnostics.HasError() {
		return
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *signingKeyResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	var state signingKeyModel
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() || deletionProtected(state.DeletionProtection, "signing key", &resp.Diagnostics) {
		return
	}
	if err := r.data.client.DeleteSigningKey(ctx, state.ID.ValueString()); err != nil && !mux.IsNotFound(err) {
		resp.Diagnostics.AddError("Unable to delete signing key", err.Error())
	}
}

// ImportState adopts an existing key by its ID. Its private key stays unknown
// to Terraform (null).
func (r *signingKeyResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), req, resp)
}
