package datasources

import (
	"context"
	"fmt"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ datasource.DataSource              = &usersDataSource{}
	_ datasource.DataSourceWithConfigure = &usersDataSource{}
)

func NewUsersDataSource() datasource.DataSource {
	return &usersDataSource{}
}

type usersDataSource struct {
	client *client.Client
}

// usersDataSourceModel holds the optional filters plus the matched users.
// Filters are applied in-process against the shared user list cache: the API
// offers no filtering beyond team membership, and the list is a single request.
type usersDataSourceModel struct {
	ID       types.Int64   `tfsdk:"id"`
	Email    types.String  `tfsdk:"email"`
	FullName types.String  `tfsdk:"full_name"`
	Role     types.String  `tfsdk:"role"`
	AuthType types.String  `tfsdk:"auth_type"`
	Active   types.Bool    `tfsdk:"active"`
	IDs      []types.Int64 `tfsdk:"ids"`
	Users    []userModel   `tfsdk:"users"`
}

type userModel struct {
	ID                 types.Int64  `tfsdk:"id"`
	Email              types.String `tfsdk:"email"`
	FullName           types.String `tfsdk:"full_name"`
	Role               types.String `tfsdk:"role"`
	AuthType           types.String `tfsdk:"auth_type"`
	Active             types.Bool   `tfsdk:"active"`
	LastLoginTimestamp types.Int64  `tfsdk:"last_login_timestamp"`
}

func (d *usersDataSource) Metadata(_ context.Context, request datasource.MetadataRequest, response *datasource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_users"
}

func (d *usersDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, response *datasource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Looks up users in the Aikido workspace. " +
			"Returns every user, active and inactive, unless filters narrow the result. " +
			"Filters combine with AND; a filter that matches nothing yields an empty list rather than an error. " +
			"No filter is guaranteed to identify exactly one account, not even email, so ids may hold zero or several entries: " +
			"use one(...) where a single user is required and the configuration should fail otherwise. " +
			"The underlying endpoint returns the whole workspace in a single unpaginated response.",
		Attributes: map[string]schema.Attribute{
			"id": schema.Int64Attribute{
				Optional:    true,
				Description: "Only return the user with this Aikido user ID.",
			},
			"email": schema.StringAttribute{
				Optional:    true,
				Description: "Only return users with exactly this email address, compared without regard to case.",
			},
			"full_name": schema.StringAttribute{
				Optional:    true,
				Description: "Only return users whose full name is exactly this.",
			},
			"role": schema.StringAttribute{
				Optional: true,
				Description: "Only return users with this workspace role. " +
					"`admin` administers the workspace, `default` sees every repository without administering it, " +
					"and `team_only` sees only the repositories of the teams it belongs to. " +
					"Team membership widens access for `team_only` users; the other two already see everything.",
			},
			"auth_type": schema.StringAttribute{
				Optional:    true,
				Description: "Only return users authenticating this way: github, gitlab, bitbucket, google, office365 or saml.",
			},
			"active": schema.BoolAttribute{
				Optional:    true,
				Description: "Only return users with this activation state. Omit to return both active and deactivated users.",
			},
			"ids": schema.SetAttribute{
				Computed:    true,
				ElementType: types.Int64Type,
				Description: "Numeric IDs of the matching users, typed to match the user_id attribute of the membership resources.",
			},
			"users": schema.ListNestedAttribute{
				Computed:    true,
				Description: "The matching users, in ID order.",
				NestedObject: schema.NestedAttributeObject{
					Attributes: map[string]schema.Attribute{
						"id": schema.Int64Attribute{
							Computed:    true,
							Description: "Aikido user ID.",
						},
						"email": schema.StringAttribute{
							Computed:    true,
							Description: "Email address of the user.",
						},
						"full_name": schema.StringAttribute{
							Computed:    true,
							Description: "Full name of the user.",
						},
						"role": schema.StringAttribute{
							Computed: true,
							Description: "Workspace role of the user. " +
								"`admin` administers the workspace, `default` sees every repository without administering it, " +
								"and `team_only` sees only the repositories of the teams it belongs to.",
						},
						"auth_type": schema.StringAttribute{
							Computed:    true,
							Description: "How the user authenticates: github, gitlab, bitbucket, google, office365 or saml.",
						},
						"active": schema.BoolAttribute{
							Computed:    true,
							Description: "Whether the user is active in the workspace.",
						},
						"last_login_timestamp": schema.Int64Attribute{
							Computed:    true,
							Description: "Unix timestamp of the user's last login.",
						},
					},
				},
			},
		},
	}
}

func (d *usersDataSource) Configure(_ context.Context, request datasource.ConfigureRequest, response *datasource.ConfigureResponse) {
	if request.ProviderData == nil {
		return
	}
	apiClient, isClient := request.ProviderData.(*client.Client)
	if !isClient {
		response.Diagnostics.AddError(
			"Unexpected provider data type",
			fmt.Sprintf("Expected *client.Client, got %T. This is a provider bug.", request.ProviderData),
		)
		return
	}
	d.client = apiClient
}

func (d *usersDataSource) Read(ctx context.Context, request datasource.ReadRequest, response *datasource.ReadResponse) {
	var config usersDataSourceModel
	response.Diagnostics.Append(request.Config.Get(ctx, &config)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(unknownUserFilterDiagnostics(config)...)
	if response.Diagnostics.HasError() {
		return
	}

	allUsers, err := users.All(ctx, d.client)
	if err != nil {
		response.Diagnostics.AddError("Error reading users", err.Error())
		return
	}

	matched, matchedIDs := matchingUsers(allUsers, config)
	config.Users = matched
	config.IDs = matchedIDs

	response.Diagnostics.Append(response.State.Set(ctx, &config)...)
}

// matchingUsers filters and maps in a single pass, so that ids stays aligned
// with users entry for entry. Both are always non-nil, so that no match yields
// an empty list rather than null.
func matchingUsers(allUsers []users.User, config usersDataSourceModel) ([]userModel, []types.Int64) {
	matched := make([]userModel, 0, len(allUsers))
	matchedIDs := make([]types.Int64, 0, len(allUsers))

	for _, apiUser := range allUsers {
		if !userMatchesFilters(apiUser, config) {
			continue
		}
		matched = append(matched, userModelFromAPI(apiUser))
		matchedIDs = append(matchedIDs, types.Int64Value(apiUser.ID))
	}

	return matched, matchedIDs
}

// unknownUserFilterDiagnostics rejects filters that are not known at read time.
// Refusing beats ignoring: an ignored filter would widen the result to the whole
// workspace, and that result feeds user_id.
func unknownUserFilterDiagnostics(config usersDataSourceModel) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	filters := []struct {
		name      string
		isUnknown bool
	}{
		{"id", config.ID.IsUnknown()},
		{"email", config.Email.IsUnknown()},
		{"full_name", config.FullName.IsUnknown()},
		{"role", config.Role.IsUnknown()},
		{"auth_type", config.AuthType.IsUnknown()},
		{"active", config.Active.IsUnknown()},
	}

	for _, filter := range filters {
		if !filter.isUnknown {
			continue
		}
		diagnostics.AddAttributeError(
			path.Root(filter.name),
			"Unknown user filter",
			fmt.Sprintf("The %s filter is not known at read time, so it cannot be used to select users. "+
				"Set it to a value that does not depend on an attribute that is only known after apply.", filter.name),
		)
	}

	return diagnostics
}

// userMatchesFilters reports whether a user satisfies every set filter. Unset
// filters never exclude anything.
func userMatchesFilters(apiUser users.User, config usersDataSourceModel) bool {
	if !config.ID.IsNull() && apiUser.ID != config.ID.ValueInt64() {
		return false
	}
	// Identity providers vary on case, so the same account can come back
	// capitalised differently than it was written in the configuration.
	if !config.Email.IsNull() && !strings.EqualFold(apiUser.Email, config.Email.ValueString()) {
		return false
	}
	if !config.FullName.IsNull() && apiUser.FullName != config.FullName.ValueString() {
		return false
	}
	if !config.Role.IsNull() && apiUser.Role != config.Role.ValueString() {
		return false
	}
	if !config.AuthType.IsNull() && apiUser.AuthType != config.AuthType.ValueString() {
		return false
	}
	if !config.Active.IsNull() && apiUser.IsActive() != config.Active.ValueBool() {
		return false
	}

	return true
}

func userModelFromAPI(apiUser users.User) userModel {
	return userModel{
		ID:                 types.Int64Value(apiUser.ID),
		Email:              types.StringValue(apiUser.Email),
		FullName:           types.StringValue(apiUser.FullName),
		Role:               nullIfEmpty(apiUser.Role),
		AuthType:           nullIfEmpty(apiUser.AuthType),
		Active:             types.BoolValue(apiUser.IsActive()),
		LastLoginTimestamp: types.Int64Value(apiUser.LastLoginTimestamp),
	}
}
