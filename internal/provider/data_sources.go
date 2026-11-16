// SPDX-License-Identifier: MPL-2.0

package provider

import (
	"context"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// baseDataSource holds the provider data every data source needs.
type baseDataSource struct {
	data *providerData
}

func (d *baseDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.data = configureData(req.ProviderData, &resp.Diagnostics)
}

// ------------------------------------------------------------ mux_environment

type environmentDataSource struct{ baseDataSource }

func newEnvironmentDataSource() datasource.DataSource { return &environmentDataSource{} }

type environmentDataModel struct {
	ID               types.String `tfsdk:"id"`
	Name             types.String `tfsdk:"name"`
	Type             types.String `tfsdk:"type"`
	OrganizationID   types.String `tfsdk:"organization_id"`
	OrganizationName types.String `tfsdk:"organization_name"`
	AccessTokenName  types.String `tfsdk:"access_token_name"`
	Permissions      types.Set    `tfsdk:"permissions"`
}

func (d *environmentDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_environment"
}

func (d *environmentDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "The Mux environment the provider's access token belongs to, and what the token may " +
			"do. Useful as a guard: a `check` or `precondition` on `type` or `name` keeps a configuration from " +
			"being applied to the wrong environment.",
		Attributes: map[string]schema.Attribute{
			"id":                schema.StringAttribute{Computed: true, MarkdownDescription: "Environment ID."},
			"name":              schema.StringAttribute{Computed: true, MarkdownDescription: "Environment name."},
			"type":              schema.StringAttribute{Computed: true, MarkdownDescription: "Environment type, e.g. `production` or `development`."},
			"organization_id":   schema.StringAttribute{Computed: true, MarkdownDescription: "ID of the organization the environment is in."},
			"organization_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the organization."},
			"access_token_name": schema.StringAttribute{Computed: true, MarkdownDescription: "Name of the access token in use."},
			"permissions": schema.SetAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "Permissions of the access token, e.g. `video:read`, `video:write`, `system:write`.",
			},
		},
	}
}

func (d *environmentDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	env, err := d.data.client.WhoAmI(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to read environment", err.Error())
		return
	}
	permissions := env.Permissions
	if permissions == nil {
		permissions = []string{}
	}
	set, diags := types.SetValueFrom(ctx, types.StringType, permissions)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &environmentDataModel{
		ID:               types.StringValue(env.EnvironmentID),
		Name:             types.StringValue(env.EnvironmentName),
		Type:             types.StringValue(env.EnvironmentType),
		OrganizationID:   types.StringValue(env.OrganizationID),
		OrganizationName: types.StringValue(env.OrganizationName),
		AccessTokenName:  types.StringValue(env.AccessTokenName),
		Permissions:      set,
	})...)
}

// ------------------------------------------------- mux_playback_restrictions

type playbackRestrictionsDataSource struct{ baseDataSource }

func newPlaybackRestrictionsDataSource() datasource.DataSource {
	return &playbackRestrictionsDataSource{}
}

type playbackRestrictionItemModel struct {
	ID        types.String    `tfsdk:"id"`
	Referrer  *referrerModel  `tfsdk:"referrer"`
	UserAgent *userAgentModel `tfsdk:"user_agent"`
	CreatedAt types.String    `tfsdk:"created_at"`
	UpdatedAt types.String    `tfsdk:"updated_at"`
}

type playbackRestrictionsDataModel struct {
	Restrictions []playbackRestrictionItemModel `tfsdk:"restrictions"`
}

func (d *playbackRestrictionsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_playback_restrictions"
}

func (d *playbackRestrictionsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists every playback restriction of the environment, with its rules. Handy for " +
			"finding the ID of a restriction to import, and for checking what an import will adopt.",
		Attributes: map[string]schema.Attribute{
			"restrictions": schema.ListNestedAttribute{
				Computed:            true,
				MarkdownDescription: "The playback restrictions, in the order Mux lists them.",
				NestedObject: schema.NestedAttributeObject{Attributes: map[string]schema.Attribute{
					"id": schema.StringAttribute{Computed: true, MarkdownDescription: "Playback restriction ID."},
					"referrer": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Which referring domains may play the videos.",
						Attributes: map[string]schema.Attribute{
							"allowed_domains": schema.SetAttribute{
								Computed:            true,
								ElementType:         types.StringType,
								MarkdownDescription: "Domains allowed to play the videos. Empty denies every domain.",
							},
							"allow_no_referrer": schema.BoolAttribute{
								Computed:            true,
								MarkdownDescription: "Whether requests without a `Referer` header are allowed.",
							},
						},
					},
					"user_agent": schema.SingleNestedAttribute{
						Computed:            true,
						MarkdownDescription: "Which user agents may play the videos.",
						Attributes: map[string]schema.Attribute{
							"allow_no_user_agent": schema.BoolAttribute{
								Computed:            true,
								MarkdownDescription: "Whether requests without a `User-Agent` header are allowed.",
							},
							"allow_high_risk_user_agent": schema.BoolAttribute{
								Computed:            true,
								MarkdownDescription: "Whether user agents Mux considers high risk are allowed.",
							},
						},
					},
					"created_at": schema.StringAttribute{Computed: true, MarkdownDescription: "When the restriction was created, in seconds since the Unix epoch."},
					"updated_at": schema.StringAttribute{Computed: true, MarkdownDescription: "When the restriction was last changed, in seconds since the Unix epoch."},
				}},
			},
		},
	}
}

func (d *playbackRestrictionsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	restrictions, err := d.data.client.ListPlaybackRestrictions(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list playback restrictions", err.Error())
		return
	}
	out := playbackRestrictionsDataModel{Restrictions: make([]playbackRestrictionItemModel, 0, len(restrictions))}
	for i := range restrictions {
		item := playbackRestrictionItemModel{
			ID:        types.StringValue(restrictions[i].ID),
			CreatedAt: types.StringValue(restrictions[i].CreatedAt),
			UpdatedAt: types.StringValue(restrictions[i].UpdatedAt),
		}
		item.Referrer, item.UserAgent = restrictionRules(ctx, &restrictions[i], &resp.Diagnostics)
		out.Restrictions = append(out.Restrictions, item)
	}
	resp.Diagnostics.Append(resp.State.Set(ctx, &out)...)
}

// ------------------------------------------------------ mux_drm_configurations

type drmConfigurationsDataSource struct{ baseDataSource }

func newDRMConfigurationsDataSource() datasource.DataSource { return &drmConfigurationsDataSource{} }

type drmConfigurationsDataModel struct {
	IDs types.List `tfsdk:"ids"`
}

func (d *drmConfigurationsDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_drm_configurations"
}

func (d *drmConfigurationsDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Lists the DRM configurations of the environment, for the `drm` playback policy. " +
			"Mux provisions them; the API cannot create or change one.",
		Attributes: map[string]schema.Attribute{
			"ids": schema.ListAttribute{
				Computed:            true,
				ElementType:         types.StringType,
				MarkdownDescription: "IDs of the DRM configurations, in the order Mux lists them. Empty when DRM is not set up.",
			},
		},
	}
}

func (d *drmConfigurationsDataSource) Read(ctx context.Context, _ datasource.ReadRequest, resp *datasource.ReadResponse) {
	configurations, err := d.data.client.ListDRMConfigurations(ctx)
	if err != nil {
		resp.Diagnostics.AddError("Unable to list DRM configurations", err.Error())
		return
	}
	ids := make([]string, 0, len(configurations))
	for _, c := range configurations {
		ids = append(ids, c.ID)
	}
	list, diags := types.ListValueFrom(ctx, types.StringType, ids)
	resp.Diagnostics.Append(diags...)
	resp.Diagnostics.Append(resp.State.Set(ctx, &drmConfigurationsDataModel{IDs: list})...)
}
