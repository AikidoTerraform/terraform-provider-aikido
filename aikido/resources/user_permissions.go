package resources

import (
	"context"
	"fmt"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                   = &userPermissionsResource{}
	_ resource.ResourceWithImportState    = &userPermissionsResource{}
	_ resource.ResourceWithConfigure      = &userPermissionsResource{}
	_ resource.ResourceWithValidateConfig = &userPermissionsResource{}
	_ resource.ResourceWithModifyPlan     = &userPermissionsResource{}
)

func NewUserPermissionsResource() resource.Resource {
	return &userPermissionsResource{}
}

type userPermissionsResource struct {
	client *client.Client
}

// capabilityDescriptions documents each permission flag. The set of flags comes
// from users.Capabilities, so adding one there is a schema change here.
var capabilityDescriptions = map[string]string{
	"can_ignore_issues":              "Whether the user can ignore issues.",
	"can_snooze_issues":              "Whether the user can snooze issues.",
	"can_change_issue_severity":      "Whether the user can change the severity of an issue.",
	"can_manage_teams":               "Whether the user can create and modify teams.",
	"can_manage_clouds":              "Whether the user can manage cloud integrations.",
	"can_manage_containers":          "Whether the user can manage container registries.",
	"can_manage_domains":             "Whether the user can manage domains.",
	"can_manage_code_quality":        "Whether the user can manage code quality settings.",
	"can_export_data":                "Whether the user can export workspace data.",
	"can_manage_endpoint_protection": "Whether the user can manage endpoint protection.",
	"can_manage_pentests":            "Whether the user can manage pentests.",
	"can_manage_repos":               "Whether the user can manage code repositories.",
}

func (r *userPermissionsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_user_permissions"
}

func (r *userPermissionsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	attributes := map[string]schema.Attribute{
		"id": schema.StringAttribute{
			Computed:    true,
			Description: "Aikido user ID, as a string. Always equal to user_id.",
			PlanModifiers: []planmodifier.String{
				stringplanmodifier.UseStateForUnknown(),
			},
		},
		"user_id": schema.Int64Attribute{
			Required:    true,
			Description: "Aikido user ID whose permissions this resource manages. The user must already exist; this resource never invites or removes anyone.",
			PlanModifiers: []planmodifier.Int64{
				int64planmodifier.RequiresReplace(),
			},
		},
		"role": schema.StringAttribute{
			Required: true,
			Description: "Workspace role. `admin` administers the workspace and is granted every capability below. " +
				"`default` sees every repository without administering the workspace. " +
				"`team_only` sees only the repositories of the teams it belongs to, and cannot manage teams, clouds, " +
				"container repositories, domains, code quality or pentests. " +
				"Setting a capability the role decides is rejected at plan time.",
			Validators: []validator.String{
				stringvalidator.OneOf(users.RoleAdmin, users.RoleDefault, users.RoleTeamOnly),
			},
		},
		"read_only": schema.BoolAttribute{
			Optional: true,
			Computed: true,
			Description: "Whether the user has read-only access to the workspace. Defaults to false when omitted. " +
				"Admins are never read-only, so setting this to true with `role = \"admin\"` is rejected.",
		},
	}

	for _, capability := range users.Capabilities {
		attributes[capability] = schema.BoolAttribute{
			Optional:    true,
			Computed:    true,
			Description: capabilityDescriptions[capability] + " Defaults to false.",
		}
	}

	response.Schema = schema.Schema{
		Description: "Manages the role and permissions of a user who already exists in Aikido. " +
			"The user is never created or deleted by this resource. " +
			"Capability attributes are authoritative: one omitted from the configuration is set to false on apply. " +
			"Removing the resource from the configuration leaves the user's permissions unchanged.",
		Attributes: attributes,
	}
}

func (r *userPermissionsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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
	r.client = apiClient
}

// ValidateConfig refuses capabilities the chosen role would override. Aikido
// stores its own value regardless of what was asked for, so accepting these
// would make every apply fail with an inconsistent-result error instead.
func (r *userPermissionsResource) ValidateConfig(ctx context.Context, request resource.ValidateConfigRequest, response *resource.ValidateConfigResponse) {
	var role types.String
	response.Diagnostics.Append(request.Config.GetAttribute(ctx, path.Root("role"), &role)...)
	if response.Diagnostics.HasError() || role.IsNull() || role.IsUnknown() {
		return
	}

	if forcedValue, forced := users.ForcedReadOnly(role.ValueString()); forced {
		response.Diagnostics.Append(conflictDiagnostics(ctx, request.Config, "read_only", forcedValue, role.ValueString())...)
	}

	for _, capability := range users.Capabilities {
		forcedValue, forced := users.ForcedCapability(role.ValueString(), capability)
		if !forced {
			continue
		}
		response.Diagnostics.Append(conflictDiagnostics(ctx, request.Config, capability, forcedValue, role.ValueString())...)
	}
}

// conflictDiagnostics reports an attribute set to something the role overrides.
// A value matching the override is left alone: it is redundant, not wrong.
func conflictDiagnostics(ctx context.Context, config tfsdk.Config, attribute string, forcedValue bool, role string) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	var configured types.Bool
	diagnostics.Append(config.GetAttribute(ctx, path.Root(attribute), &configured)...)
	if diagnostics.HasError() || configured.IsNull() || configured.IsUnknown() {
		return diagnostics
	}
	if configured.ValueBool() == forcedValue {
		return diagnostics
	}

	diagnostics.AddAttributeError(
		path.Root(attribute),
		"Attribute conflicts with the role",
		fmt.Sprintf("Role %q always sets %s to %t, so %t cannot be applied. "+
			"Aikido would store %t regardless, and every later plan would report a difference it can never resolve. "+
			"Remove %s from this resource, or choose a role that leaves it to you.",
			role, attribute, forcedValue, configured.ValueBool(), forcedValue, attribute),
	)

	return diagnostics
}

// ModifyPlan writes the values Aikido will actually store into the plan: the
// ones the role forces, and false for anything the configuration omits. Without
// this the plan would show an omitted capability keeping its old value, which is
// not what gets written.
func (r *userPermissionsResource) ModifyPlan(ctx context.Context, request resource.ModifyPlanRequest, response *resource.ModifyPlanResponse) {
	if request.Plan.Raw.IsNull() {
		return // destroy: there is no plan to normalise
	}

	var role types.String
	response.Diagnostics.Append(request.Plan.GetAttribute(ctx, path.Root("role"), &role)...)
	if response.Diagnostics.HasError() || role.IsNull() || role.IsUnknown() {
		return
	}

	forcedReadOnly, readOnlyForced := users.ForcedReadOnly(role.ValueString())
	response.Diagnostics.Append(planAttribute(ctx, request.Config, &response.Plan, "read_only", forcedReadOnly, readOnlyForced)...)

	for _, capability := range users.Capabilities {
		forcedValue, forced := users.ForcedCapability(role.ValueString(), capability)
		response.Diagnostics.Append(planAttribute(ctx, request.Config, &response.Plan, capability, forcedValue, forced)...)
	}
}

// planAttribute fixes one boolean in the plan: to the role's value when the role
// forces it, and to false when the configuration leaves it out.
func planAttribute(ctx context.Context, config tfsdk.Config, plan *tfsdk.Plan, attribute string, forcedValue, forced bool) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	if forced {
		return plan.SetAttribute(ctx, path.Root(attribute), types.BoolValue(forcedValue))
	}

	var configured types.Bool
	diagnostics.Append(config.GetAttribute(ctx, path.Root(attribute), &configured)...)
	if diagnostics.HasError() || !configured.IsNull() {
		return diagnostics
	}

	diagnostics.Append(plan.SetAttribute(ctx, path.Root(attribute), types.BoolValue(false))...)

	return diagnostics
}

func (r *userPermissionsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	r.write(ctx, request.Plan, &response.State, &response.Diagnostics)
}

func (r *userPermissionsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	r.write(ctx, request.Plan, &response.State, &response.Diagnostics)
}

// write applies the planned rights and stores what Aikido reports back, so state
// records what was saved rather than what was asked for.
func (r *userPermissionsResource) write(ctx context.Context, plan tfsdk.Plan, state *tfsdk.State, diagnostics *diag.Diagnostics) {
	planned, readDiagnostics := plannedRights(ctx, plan)
	diagnostics.Append(readDiagnostics...)
	if diagnostics.HasError() {
		return
	}

	effective := users.Effective(planned.role, planned.capabilities)
	if err := users.UpdateRights(ctx, r.client, planned.userID, planned.role, planned.readOnly, effective); err != nil {
		diagnostics.AddError("Error updating user permissions", err.Error())
		return
	}

	details, err := users.Detail(ctx, r.client, planned.userID)
	if err != nil {
		diagnostics.AddError("Error reading user permissions back", err.Error())
		return
	}

	diagnostics.Append(setStateFromDetails(ctx, state, details)...)
}

func (r *userPermissionsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var userID types.Int64
	response.Diagnostics.Append(request.State.GetAttribute(ctx, path.Root("user_id"), &userID)...)
	if response.Diagnostics.HasError() {
		return
	}

	details, err := users.Detail(ctx, r.client, userID.ValueInt64())
	if err != nil {
		if client.NotFound(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading user permissions", err.Error())
		return
	}

	response.Diagnostics.Append(setStateFromDetails(ctx, &response.State, details)...)
}

// Delete stops managing the user without touching their permissions. There is
// no permissions object to remove — the user outlives this resource — and
// revoking access as a side effect of a destroy is how people get locked out.
func (r *userPermissionsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var userID types.Int64
	response.Diagnostics.Append(request.State.GetAttribute(ctx, path.Root("user_id"), &userID)...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.AddWarning(
		"User permissions left unchanged",
		fmt.Sprintf("Terraform no longer manages the permissions of user %d, but Aikido still has them exactly as this "+
			"resource last applied them, including the role. Change them in Aikido if they should not persist.",
			userID.ValueInt64()),
	)
}

// ImportState adopts an existing user by their numeric ID. The first read fills
// in the role and every capability.
func (r *userPermissionsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	userID, err := strconv.ParseInt(request.ID, 10, 64)
	if err != nil {
		response.Diagnostics.AddError(
			"Invalid import ID",
			fmt.Sprintf("Expected a numeric Aikido user ID, got %q.", request.ID),
		)
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), types.StringValue(request.ID))...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("user_id"), types.Int64Value(userID))...)
}

// rights is one user's full set of managed values, as planned.
type rights struct {
	userID       int64
	role         string
	readOnly     bool
	capabilities map[string]bool
}

func plannedRights(ctx context.Context, plan tfsdk.Plan) (rights, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	var userID types.Int64
	var role types.String
	var readOnly types.Bool
	diagnostics.Append(plan.GetAttribute(ctx, path.Root("user_id"), &userID)...)
	diagnostics.Append(plan.GetAttribute(ctx, path.Root("role"), &role)...)
	diagnostics.Append(plan.GetAttribute(ctx, path.Root("read_only"), &readOnly)...)
	if diagnostics.HasError() {
		return rights{}, diagnostics
	}

	capabilities := make(map[string]bool, len(users.Capabilities))
	for _, capability := range users.Capabilities {
		var value types.Bool
		diagnostics.Append(plan.GetAttribute(ctx, path.Root(capability), &value)...)
		if diagnostics.HasError() {
			return rights{}, diagnostics
		}
		capabilities[capability] = value.ValueBool()
	}

	return rights{
		userID:       userID.ValueInt64(),
		role:         role.ValueString(),
		readOnly:     readOnly.ValueBool(),
		capabilities: capabilities,
	}, diagnostics
}

// setStateFromDetails records what the API reports. Capabilities the response
// omits are stored as false rather than left unknown, so state is always
// complete even if Aikido stops sending one.
func setStateFromDetails(ctx context.Context, state *tfsdk.State, details users.Details) diag.Diagnostics {
	var diagnostics diag.Diagnostics

	diagnostics.Append(state.SetAttribute(ctx, path.Root("id"), types.StringValue(strconv.FormatInt(details.ID, 10)))...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("user_id"), types.Int64Value(details.ID))...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("role"), types.StringValue(details.Role))...)
	diagnostics.Append(state.SetAttribute(ctx, path.Root("read_only"), types.BoolValue(bool(details.ReadOnly)))...)

	for _, capability := range users.Capabilities {
		diagnostics.Append(state.SetAttribute(ctx, path.Root(capability), types.BoolValue(reportedCapability(details, capability)))...)
	}

	return diagnostics
}

// reportedCapability reads one capability from a detail response, falling back
// to the value the role fixes when the response omits it. The API sends every
// capability, so the fallback only matters if that changes.
func reportedCapability(details users.Details, capability string) bool {
	if value, present := details.Permissions[capability]; present {
		return bool(value)
	}
	if forcedValue, forced := users.ForcedCapability(details.Role, capability); forced {
		return forcedValue
	}

	return false
}
