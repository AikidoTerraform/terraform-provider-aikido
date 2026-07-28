package autofix_settings

import (
	"context"
	"fmt"

	"github.com/aikido/terraform-provider-aikido/internal/client"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const autofixSettingsResourceID = "autofix_settings"

var (
	_ resource.Resource                = &autofixSettingsResource{}
	_ resource.ResourceWithImportState = &autofixSettingsResource{}
	_ resource.ResourceWithConfigure   = &autofixSettingsResource{}
)

func NewResource() resource.Resource {
	return &autofixSettingsResource{}
}

type autofixSettingsResource struct {
	client *client.Client
}

type autofixSettingsModel struct {
	ID         types.String     `tfsdk:"id"`
	Dependency *dependencyModel `tfsdk:"dependency"`
	Sast       *sastModel       `tfsdk:"sast"`
	Pentest    *pentestModel    `tfsdk:"pentest"`
}

func (r *autofixSettingsResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_autofix_settings"
}

func (r *autofixSettingsResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages workspace Autofix settings for automatic AutoFix PR creation. " +
			"Configure dependency (SCA), SAST & IaC, and pentest autofix via the dependency, sast, and pentest nested attributes (all required). " +
			"Each nested attribute maps to its own Autofix settings API endpoint. " +
			"When a nested attribute's enabled is false, that feature is disabled and its other fields are ignored by the API. " +
			"Repo ID sets are ignored when the corresponding repos_scope is all.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Workspace Autofix settings identifier.",
			},
			"dependency": dependencySchemaAttribute(),
			"sast":       sastSchemaAttribute(),
			"pentest":    pentestSchemaAttribute(),
		},
	}
}

func (r *autofixSettingsResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *autofixSettingsResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned autofixSettingsModel

	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applySettings(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixSettingsResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState autofixSettingsModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.readSettings(ctx, &priorState)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	if state == nil {
		response.State.RemoveResource(ctx)
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixSettingsResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned autofixSettingsModel

	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, diags := r.applySettings(ctx, planned)
	response.Diagnostics.Append(diags...)
	if response.Diagnostics.HasError() {
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

func (r *autofixSettingsResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState autofixSettingsModel

	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	// Destroy disables automatic PR creation for each Autofix feature.
	disabledDependency := &dependencyModel{Enabled: types.BoolValue(false)}
	disabledSast := &sastModel{Enabled: types.BoolValue(false)}
	disabledPentest := &pentestModel{Enabled: types.BoolValue(false)}

	if err := upsertScaSettings(ctx, r.client, disabledDependency); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling dependency autofix settings", err.Error())
		return
	}

	if err := upsertSastSettings(ctx, r.client, disabledSast); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling SAST autofix settings", err.Error())
		return
	}

	if err := upsertPentestSettings(ctx, r.client, disabledPentest); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error disabling pentest autofix settings", err.Error())
	}
}

// ImportState lets users adopt existing workspace Autofix settings into state.
func (r *autofixSettingsResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

// applySettings upserts each feature endpoint, then refreshes state from GET.
func (r *autofixSettingsResource) applySettings(ctx context.Context, planned autofixSettingsModel) (autofixSettingsModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	if err := upsertScaSettings(ctx, r.client, planned.Dependency); err != nil {
		diags.AddError("Error configuring dependency autofix settings", err.Error())
		return autofixSettingsModel{}, diags
	}

	if err := upsertSastSettings(ctx, r.client, planned.Sast); err != nil {
		diags.AddError("Error configuring SAST autofix settings", err.Error())
		return autofixSettingsModel{}, diags
	}

	if err := upsertPentestSettings(ctx, r.client, planned.Pentest); err != nil {
		diags.AddError("Error configuring pentest autofix settings", err.Error())
		return autofixSettingsModel{}, diags
	}

	state, readDiags := r.readSettings(ctx, &planned)
	diags.Append(readDiags...)
	if diags.HasError() || state == nil {
		return autofixSettingsModel{}, diags
	}

	// Prefer the plan over GET for fields the API may rewrite when unused
	// (enabled=false, or repos_scope=all). Otherwise Terraform reports an
	// inconsistent result after apply.
	applyScaPlanOverrides(state.Dependency, planned.Dependency)
	applySastPlanOverrides(state.Sast, planned.Sast)
	applyPentestPlanOverrides(state.Pentest, planned.Pentest)

	return *state, diags
}

func (r *autofixSettingsResource) readSettings(ctx context.Context, prior *autofixSettingsModel) (*autofixSettingsModel, diag.Diagnostics) {
	var diags diag.Diagnostics

	scaAPI, err := getScaSettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading dependency autofix settings", err.Error())
		return nil, diags
	}

	sastAPI, err := getSastSettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading SAST autofix settings", err.Error())
		return nil, diags
	}

	pentestAPI, err := getPentestSettings(ctx, r.client)
	if err != nil {
		if client.NotFound(err) {
			return nil, diags
		}

		diags.AddError("Error reading pentest autofix settings", err.Error())
		return nil, diags
	}

	var priorDependency *dependencyModel
	var priorSast *sastModel
	var priorPentest *pentestModel

	if prior != nil {
		priorDependency = prior.Dependency
		priorSast = prior.Sast
		priorPentest = prior.Pentest
	}

	return &autofixSettingsModel{
		ID:         types.StringValue(autofixSettingsResourceID),
		Dependency: mergeScaAPIAndPrior(scaAPI, priorDependency),
		Sast:       mergeSastAPIAndPrior(sastAPI, priorSast),
		Pentest:    mergePentestAPIAndPrior(pentestAPI, priorPentest),
	}, diags
}

func normalizeIDs(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}

	return ids
}
