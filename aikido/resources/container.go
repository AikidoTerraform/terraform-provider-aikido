package resources

import (
	"context"
	"fmt"
	"net/http"
	"strconv"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

const containerBasePath = containers.BasePath

var (
	_ resource.Resource                = &containerResource{}
	_ resource.ResourceWithImportState = &containerResource{}
	_ resource.ResourceWithConfigure   = &containerResource{}
)

func NewContainerResource() resource.Resource {
	return &containerResource{}
}

type containerResource struct {
	client *client.Client
}

// containerModel is the Terraform state. IDs are strings by TF convention even
// though the API uses integers.
type containerModel struct {
	ID                types.String   `tfsdk:"id"`
	Active            types.Bool     `tfsdk:"active"`
	TagFilter         types.String   `tfsdk:"tag_filter"`
	Sensitivity       types.String   `tfsdk:"sensitivity"`
	Connectivity      types.String   `tfsdk:"connectivity"`
	Labels            []types.String `tfsdk:"labels"`
	LinkedCodeRepoID  types.Int64    `tfsdk:"linked_code_repo_id"`
	Name              types.String   `tfsdk:"name"`
	RegistryProvider  types.String   `tfsdk:"registry_provider"`
	RegistryID        types.Int64    `tfsdk:"registry_id"`
	RegistryName      types.String   `tfsdk:"registry_name"`
	CloudID           types.Int64    `tfsdk:"cloud_id"`
	Distro            types.String   `tfsdk:"distro"`
	DistroVersion     types.String   `tfsdk:"distro_version"`
	LastScannedAt     types.Int64    `tfsdk:"last_scanned_at"`
	LastScannedTag    types.String   `tfsdk:"last_scanned_tag"`
	LastScannedDigest types.String   `tfsdk:"last_scanned_digest"`
}

func (r *containerResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_container"
}

func (r *containerResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Manages activation and configuration of an existing Aikido container. " +
			"The container must already exist in Aikido (discovered from a registry or a cloud); this resource never creates or deletes it. " +
			"Apply sets active and optional config; destroy deactivates it.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Required:    true,
				Description: "Aikido container ID.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"active": schema.BoolAttribute{
				Required: true,
				Description: "Whether the container is activated for scanning in Aikido. " +
					"Public images and self-managed SBOM uploads cannot be deactivated: " +
					"Aikido rejects the request and they have to be deleted instead.",
			},
			"tag_filter": schema.StringAttribute{
				Optional: true,
				Computed: true,
				Description: "Tag filter deciding which image is scanned. " +
					"Supports * wildcards, for example prod-*, and the special value semver-production. " +
					"Omitting it leaves the container's current filter alone; set it to the empty string to scan the newest image instead. " +
					"Aikido rejects a tag filter on public images and on self-managed SBOM uploads, " +
					"and on a container whose clone in the same region already carries the same filter.",
			},
			"sensitivity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Sensitivity level of the container. One of: extreme, sensitive, normal, not_sensitive, no_data.",
				Validators: []validator.String{
					stringvalidator.OneOf("extreme", "sensitive", "normal", "not_sensitive", "no_data"),
				},
			},
			"connectivity": schema.StringAttribute{
				Optional:    true,
				Computed:    true,
				Description: "Whether the container runs on an internet-connected server. One of: connected, not_connected, unknown.",
				Validators: []validator.String{
					stringvalidator.OneOf("connected", "not_connected", "unknown"),
				},
			},
			"labels": labelsSchemaAttribute("container"),
			"linked_code_repo_id": schema.Int64Attribute{
				Optional: true,
				Computed: true,
				Description: "Aikido code repository ID to link to this container. " +
					"Written only when set; removing it from the configuration leaves the existing link in place rather than unlinking.",
			},
			"name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the container.",
			},
			"registry_provider": schema.StringAttribute{
				Computed:    true,
				Description: "Provider hosting the container (e.g. aws, acr, docker-hub).",
			},
			"registry_id": schema.Int64Attribute{
				Computed:    true,
				Description: "ID of the registry holding this container. Null when the container was discovered through a cloud.",
			},
			"registry_name": schema.StringAttribute{
				Computed:    true,
				Description: "Name of the registry holding this container. For AWS this is the account ID.",
			},
			"cloud_id": schema.Int64Attribute{
				Computed:    true,
				Description: "ID of the cloud acting as the registry for this container. Null when the container came from a registry.",
			},
			"distro": schema.StringAttribute{
				Computed:    true,
				Description: "Linux distribution detected in the scanned image.",
			},
			"distro_version": schema.StringAttribute{
				Computed:    true,
				Description: "Version of the detected distribution.",
			},
			"last_scanned_at": schema.Int64Attribute{
				Computed:    true,
				Description: "Unix timestamp of the last completed scan.",
			},
			"last_scanned_tag": schema.StringAttribute{
				Computed:    true,
				Description: "Tag that was actually scanned, as opposed to the tag_filter that selected it.",
			},
			"last_scanned_digest": schema.StringAttribute{
				Computed:    true,
				Description: "Digest of the scanned image.",
			},
		},
	}
}

func (r *containerResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

// Create is called on first apply when the resource is in config but not yet in
// state. It activates/deactivates and configures an existing Aikido container.
func (r *containerResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned containerModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setContainerConfig(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring container", err.Error())
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// Read is called during refresh/plan to sync the container from the API into state.
func (r *containerResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState containerModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseContainerID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading container", err.Error())
		return
	}

	apiContainer, err := containers.ByID(ctx, r.client, id)
	if err != nil {
		// Only a successful list proves the container is gone. A failed request
		// must not remove a live resource from state.
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading container", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, containerReadState(apiContainer, priorState.Labels))...)
}

// Update is called on apply when config changes in-place (no replacement).
func (r *containerResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned containerModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	state, err := r.setContainerConfig(ctx, planned)
	if err != nil {
		response.Diagnostics.AddError("Error configuring container", err.Error())
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, state)...)
}

// Delete is called when the resource is removed from config or on destroy.
// Deactivates the container; it is never deleted from Aikido.
func (r *containerResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState containerModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseContainerID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deactivating container", err.Error())
		return
	}

	defer containers.InvalidateCache(r.client)

	if err := r.setActive(ctx, id, false); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deactivating container", deactivationFailureDetail(err))
	}
}

// ImportState lets users adopt an existing Aikido container into state by ID.
func (r *containerResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	resource.ImportStatePassthroughID(ctx, path.Root("id"), request, response)
}

// setContainerConfig is shared by Create and Update.
func (r *containerResource) setContainerConfig(ctx context.Context, planned containerModel) (containerModel, error) {
	id, err := parseContainerID(planned.ID.ValueString())
	if err != nil {
		return containerModel{}, err
	}

	// Read before writing, so every write can be skipped when the container
	// already holds the planned value. The shared list is the read that carries
	// sensitivity and connectivity, which the detail endpoint omits, and it costs
	// one paginated fetch across every container in the plan.
	current, err := containers.ByID(ctx, r.client, id)
	if err != nil {
		return containerModel{}, err
	}

	updated, err := r.writeContainerChanges(ctx, id, planned, current)
	if err != nil {
		// A write that failed partway may still have changed the container, so the
		// list is dropped rather than refreshed from the plan.
		containers.InvalidateCache(r.client)

		return containerModel{}, err
	}
	containers.StoreCached(r.client, updated)

	state := containerModelFromAPI(updated)
	state.Labels = planned.Labels
	if isManaged(planned.TagFilter) {
		state.TagFilter = planned.TagFilter
	}
	state.Sensitivity = firstKnownString(state.Sensitivity, planned.Sensitivity)
	state.Connectivity = firstKnownString(state.Connectivity, planned.Connectivity)
	if isManagedInt64(planned.LinkedCodeRepoID) {
		state.LinkedCodeRepoID = planned.LinkedCodeRepoID
	}

	return state, nil
}

// writeContainerChanges sends only the writes the container actually needs and
// returns it as it now stands.
//
// A write the container does not need is not free. Aikido stamps
// manually_toggled_active_at before it checks whether activation changes
// anything, and that stamp excludes the container from auto-deactivation for
// good; relinking a code repository makes it redo the associated issues and
// AutoFix metadata.
func (r *containerResource) writeContainerChanges(
	ctx context.Context,
	id int64,
	planned containerModel,
	current containers.Container,
) (containers.Container, error) {
	updated := current

	// Activation first, so the remaining writes land on an active container.
	if planned.Active.ValueBool() != current.Active {
		if err := r.setActive(ctx, id, planned.Active.ValueBool()); err != nil {
			return updated, err
		}
		updated.Active = planned.Active.ValueBool()
	}

	if tagFilterChanged(planned.TagFilter, current.TagFilter) {
		if err := r.setTagFilter(ctx, id, planned.TagFilter); err != nil {
			return updated, fmt.Errorf("updating tag filter: %w", err)
		}
		updated.TagFilter = planned.TagFilter.ValueString()
	}

	if changedString(planned.Sensitivity, current.Sensitivity) {
		body := map[string]string{"sensitivity": planned.Sensitivity.ValueString()}
		if err := r.client.Do(ctx, http.MethodPut, containers.DetailPath(id)+"/sensitivity", body, nil); err != nil {
			return updated, fmt.Errorf("updating sensitivity: %w", err)
		}
		updated.Sensitivity = planned.Sensitivity.ValueString()
	}
	if changedString(planned.Connectivity, current.Connectivity) {
		body := map[string]string{"internet_exposed": planned.Connectivity.ValueString()}
		if err := r.client.Do(ctx, http.MethodPut, containers.DetailPath(id)+"/internetConnection", body, nil); err != nil {
			return updated, fmt.Errorf("updating connectivity: %w", err)
		}
		updated.Connectivity = planned.Connectivity.ValueString()
	}
	if changedInt64(planned.LinkedCodeRepoID, current.LinkedCodeRepoID) {
		body := map[string]int64{
			"container_repo_id": id,
			"code_repo_id":      planned.LinkedCodeRepoID.ValueInt64(),
		}
		if err := r.client.Do(ctx, http.MethodPost, containers.BasePath+"/linkCodeRepo", body, nil); err != nil {
			return updated, fmt.Errorf("linking code repository: %w", err)
		}
		linked := planned.LinkedCodeRepoID.ValueInt64()
		updated.LinkedCodeRepoID = &linked
	}

	if err := applyLabels(ctx, r.client, containerBasePath, planned.ID.ValueString(), planned.Labels, current.Labels); err != nil {
		return updated, err
	}
	updated.Labels = reconciledLabels(planned.Labels, current.Labels)

	return updated, nil
}

// changedString reports whether a managed string attribute differs from what the
// container holds. An unmanaged attribute changes nothing.
func changedString(planned types.String, current string) bool {
	return isManaged(planned) && planned.ValueString() != current
}

func changedInt64(planned types.Int64, current *int64) bool {
	return isManagedInt64(planned) && (current == nil || *current != planned.ValueInt64())
}

// reconciledLabels is the set applyLabels leaves behind: the planned names,
// carrying the IDs of those that already existed, plus the imported labels it
// never deletes. A name created in this call has no ID yet, and only names are
// read back.
func reconciledLabels(planned []types.String, current []containers.Label) []containers.Label {
	if planned == nil {
		return current
	}

	currentByName := make(map[string]containers.Label, len(current))
	for _, label := range current {
		currentByName[label.Name] = label
	}

	plannedNames := make(map[string]struct{}, len(planned))
	reconciled := make([]containers.Label, 0, len(planned)+len(current))
	for _, name := range planned {
		plannedNames[name.ValueString()] = struct{}{}
		if existing, ok := currentByName[name.ValueString()]; ok {
			reconciled = append(reconciled, existing)
			continue
		}
		reconciled = append(reconciled, containers.Label{Name: name.ValueString()})
	}
	for _, label := range current {
		if !label.IsImported {
			continue
		}
		if _, isPlanned := plannedNames[label.Name]; isPlanned {
			continue
		}
		reconciled = append(reconciled, label)
	}

	return reconciled
}

// containerReadState composes the state for a refresh. Labels omitted from the
// configuration are unmanaged, so a nil prior set stays nil.
func containerReadState(apiContainer containers.Container, priorLabels []types.String) containerModel {
	state := containerModelFromAPI(apiContainer)
	if priorLabels == nil {
		state.Labels = nil
	}

	return state
}

// deactivationFailureDetail adds the way forward to a refused deactivation.
// Aikido refuses to deactivate public images and self-managed SBOM uploads, and
// nothing in a container read predicts it, so the remedy cannot be reported
// before the call is made.
func deactivationFailureDetail(err error) string {
	if !client.BadRequest(err) {
		return err.Error()
	}

	return err.Error() + "\n\n" +
		"Aikido refuses to deactivate public images and self-managed SBOM uploads. " +
		"Delete the container in Aikido and destroy again, or run terraform state rm " +
		"to stop managing it while it stays active in Aikido."
}

// setActive activates or deactivates the container.
func (r *containerResource) setActive(ctx context.Context, id int64, isActive bool) error {
	endpoint := containers.BasePath + "/deactivate"
	if isActive {
		endpoint = containers.BasePath + "/activate"
	}

	return r.client.Do(ctx, http.MethodPost, endpoint, map[string]int64{"container_repo_id": id}, nil)
}

// setTagFilter writes the tag filter. Null is what tells the API to scan the
// newest image.
func (r *containerResource) setTagFilter(ctx context.Context, id int64, tagFilter types.String) error {
	body := struct {
		ContainerRepoID int64   `json:"container_repo_id"`
		TagFilter       *string `json:"tag_filter"`
	}{ContainerRepoID: id}

	if value := tagFilter.ValueString(); value != "" {
		body.TagFilter = &value
	}

	return r.client.Do(ctx, http.MethodPost, containers.BasePath+"/updateTagFilter", body, nil)
}

// tagFilterChanged reports whether the configuration asks for a filter the
// container does not already carry. Aikido rejects updateTagFilter on public
// images, on custom SBOM uploads, and when a sibling clone in the same region
// already scans the tag, and those guards fire on a no-op write too.
func tagFilterChanged(planned types.String, current string) bool {
	return isManaged(planned) && planned.ValueString() != current
}

// isManaged reports whether a string attribute carries a value to write.
func isManaged(value types.String) bool {
	return !value.IsNull() && !value.IsUnknown()
}

func isManagedInt64(value types.Int64) bool {
	return !value.IsNull() && !value.IsUnknown()
}

// firstKnownString prefers the value the API reported, falling back to the planned
// value and finally to null. Null is a legal value for a Computed attribute;
// unknown after apply is not.
func firstKnownString(fromAPI, planned types.String) types.String {
	if isManaged(fromAPI) {
		return fromAPI
	}
	if isManaged(planned) {
		return planned
	}

	return types.StringNull()
}

func parseContainerID(containerID string) (int64, error) {
	id, err := strconv.ParseInt(containerID, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid container id %q: %w", containerID, err)
	}

	return id, nil
}

// containerModelFromAPI maps an API container into a Terraform state model.
func containerModelFromAPI(apiContainer containers.Container) containerModel {
	return containerModel{
		ID:                types.StringValue(strconv.FormatInt(apiContainer.ID, 10)),
		Active:            types.BoolValue(apiContainer.Active),
		TagFilter:         types.StringValue(apiContainer.TagFilter),
		Sensitivity:       nullIfEmptyString(apiContainer.Sensitivity),
		Connectivity:      nullIfEmptyString(apiContainer.Connectivity),
		LinkedCodeRepoID:  nullableInt64(apiContainer.LinkedCodeRepoID),
		Name:              types.StringValue(apiContainer.Name),
		RegistryProvider:  types.StringValue(apiContainer.Provider),
		RegistryID:        nullableInt64(apiContainer.RegistryID),
		RegistryName:      nullIfEmptyString(apiContainer.RegistryName),
		CloudID:           nullableInt64(apiContainer.CloudID),
		Distro:            nullIfEmptyString(apiContainer.Distro),
		DistroVersion:     nullIfEmptyString(apiContainer.DistroVersion),
		LastScannedAt:     types.Int64Value(apiContainer.LastScannedAt),
		LastScannedTag:    nullIfEmptyString(apiContainer.LastScannedTag),
		LastScannedDigest: nullIfEmptyString(apiContainer.LastScannedDigest),
		Labels:            labelNamesFromAPI(apiContainer.Labels),
	}
}

func nullIfEmptyString(value string) types.String {
	if value == "" {
		return types.StringNull()
	}

	return types.StringValue(value)
}

// nullableInt64 keeps an absent ID null rather than 0, so that a missing registry
// is not mistaken for registry 0.
func nullableInt64(value *int64) types.Int64 {
	if value == nil {
		return types.Int64Null()
	}

	return types.Int64Value(*value)
}
