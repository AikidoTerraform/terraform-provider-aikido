package resources

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/int64planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

var (
	_ resource.Resource                = &containerCloneResource{}
	_ resource.ResourceWithImportState = &containerCloneResource{}
	_ resource.ResourceWithConfigure   = &containerCloneResource{}
)

func NewContainerCloneResource() resource.Resource {
	return &containerCloneResource{}
}

type containerCloneResource struct {
	client *client.Client
}

type containerCloneModel struct {
	ID                types.String `tfsdk:"id"`
	SourceContainerID types.Int64  `tfsdk:"source_container_id"`
	TagFilter         types.String `tfsdk:"tag_filter"`
}

func (r *containerCloneResource) Metadata(_ context.Context, request resource.MetadataRequest, response *resource.MetadataResponse) {
	response.TypeName = request.ProviderTypeName + "_container_clone"
}

func (r *containerCloneResource) Schema(_ context.Context, _ resource.SchemaRequest, response *resource.SchemaResponse) {
	response.Schema = schema.Schema{
		Description: "Clones an existing Aikido container with its own tag filter, so that several tags of one image are scanned. " +
			"Only containers from a connected registry can be cloned. " +
			"The clone starts active and is scanned right away; manage its other settings with an aikido_container on the clone's ID. " +
			"Destroy deactivates the clone, which stays available in Aikido. " +
			"To scan that tag again, reactivate the existing clone with an aikido_container on its ID rather than cloning it again. " +
			"Clones already made in the Aikido UI can be imported as <source_container_id>:<clone_id>; " +
			"the aikido_containers data source finds a clone's ID by the source container's name and registry plus the clone's tag_filter.",
		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				Computed:    true,
				Description: "Aikido container ID of the clone.",
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"source_container_id": schema.Int64Attribute{
				Required:    true,
				Description: "Aikido container ID of the container to clone.",
				Validators: []validator.Int64{
					int64validator.AtLeast(1),
				},
				PlanModifiers: []planmodifier.Int64{
					int64planmodifier.RequiresReplace(),
				},
			},
			"tag_filter": schema.StringAttribute{
				Required: true,
				Description: "Tag filter deciding which image the clone scans. " +
					"Supports * wildcards, for example prod-*, and the special value semver-production. " +
					"Aikido rejects a tag that another clone of the same image in the same registry already carries.",
				Validators: []validator.String{
					// Aikido trims the tag it stores, so surrounding whitespace would read
					// back as drift and replace the clone on every apply.
					stringvalidator.RegexMatches(
						regexp.MustCompile(`^\S(.*\S)?$`),
						"must not be empty or start or end with whitespace",
					),
				},
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
		},
	}
}

func (r *containerCloneResource) Configure(_ context.Context, request resource.ConfigureRequest, response *resource.ConfigureResponse) {
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

func (r *containerCloneResource) Create(ctx context.Context, request resource.CreateRequest, response *resource.CreateResponse) {
	var planned containerCloneModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}

	cloneID, diagnostics := r.createClone(ctx, planned.SourceContainerID.ValueInt64(), planned.TagFilter.ValueString())
	response.Diagnostics.Append(diagnostics...)
	if response.Diagnostics.HasError() {
		return
	}

	planned.ID = types.StringValue(strconv.FormatInt(cloneID, 10))
	response.Diagnostics.Append(response.State.Set(ctx, planned)...)
}

func (r *containerCloneResource) Read(ctx context.Context, request resource.ReadRequest, response *resource.ReadResponse) {
	var priorState containerCloneModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseContainerID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error reading container clone", err.Error())
		return
	}

	apiContainer, err := containers.ByID(ctx, r.client, id)
	if err != nil {
		// Only a successful list proves the clone is gone. A failed request must
		// not remove a live resource from state.
		if client.NotInList(err) {
			response.State.RemoveResource(ctx)
			return
		}
		response.Diagnostics.AddError("Error reading container clone", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.Set(ctx, containerCloneReadState(apiContainer, priorState))...)
}

// Update is never reached with a change: every configurable attribute forces a
// replacement.
func (r *containerCloneResource) Update(ctx context.Context, request resource.UpdateRequest, response *resource.UpdateResponse) {
	var planned containerCloneModel
	response.Diagnostics.Append(request.Plan.Get(ctx, &planned)...)
	if response.Diagnostics.HasError() {
		return
	}
	response.Diagnostics.Append(response.State.Set(ctx, planned)...)
}

// Delete deactivates the clone. It stays in Aikido, ready to be reactivated with
// an aikido_container on its ID.
func (r *containerCloneResource) Delete(ctx context.Context, request resource.DeleteRequest, response *resource.DeleteResponse) {
	var priorState containerCloneModel
	response.Diagnostics.Append(request.State.Get(ctx, &priorState)...)
	if response.Diagnostics.HasError() {
		return
	}

	id, err := parseContainerID(priorState.ID.ValueString())
	if err != nil {
		response.Diagnostics.AddError("Error deactivating container clone", err.Error())
		return
	}

	if err := r.deactivateClone(ctx, id); err != nil && !client.NotFound(err) {
		response.Diagnostics.AddError("Error deactivating container clone", err.Error())
	}
}

// ImportState adopts an existing clone, including one made in the Aikido UI. The
// ID names the source too, since the clone's source is recorded only in state.
func (r *containerCloneResource) ImportState(ctx context.Context, request resource.ImportStateRequest, response *resource.ImportStateResponse) {
	sourceID, cloneID, err := parseContainerCloneImportID(request.ID)
	if err != nil {
		response.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	if err := r.verifyCloneOfSource(ctx, sourceID, cloneID); err != nil {
		response.Diagnostics.AddError("Error importing container clone", err.Error())
		return
	}

	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("id"), strconv.FormatInt(cloneID, 10))...)
	response.Diagnostics.Append(response.State.SetAttribute(ctx, path.Root("source_container_id"), sourceID)...)
}

// createClone clones the source container and returns the clone's ID. When the
// clone request fails in a way that leaves its outcome open, the clone may exist
// all the same, and recording it here is what keeps a later apply from being
// rejected as a duplicate while the clone goes unmanaged.
func (r *containerCloneResource) createClone(ctx context.Context, sourceID int64, tagFilter string) (int64, diag.Diagnostics) {
	var diagnostics diag.Diagnostics

	cloneID, cloneErr := r.cloneContainer(ctx, sourceID, tagFilter)
	if cloneErr == nil {
		return cloneID, diagnostics
	}
	if !cloneOutcomeUncertain(cloneErr) {
		diagnostics.AddError("Error cloning container", cloneErr.Error())
		return 0, diagnostics
	}

	recoveredID, lookupErr := r.findClone(ctx, sourceID, tagFilter)
	if lookupErr != nil {
		diagnostics.AddError("Error cloning container", fmt.Sprintf(
			"%s\n\nThe clone may still have been created, and looking it up afterwards did not settle it: %s\n\n"+
				"If the clone exists, adopt it with terraform import using %d:<clone_id>.",
			cloneErr, lookupErr, sourceID,
		))
		return 0, diagnostics
	}

	diagnostics.AddWarning("Container clone recovered", fmt.Sprintf(
		"The clone request did not complete: %s\n\nClone %d with tag filter %q was found and is now managed by this resource.",
		cloneErr, recoveredID, tagFilter,
	))

	return recoveredID, diagnostics
}

// cloneOutcomeUncertain reports whether a failed clone request may still have
// created the clone. Aikido runs every check before it saves the clone, so a 4xx
// means nothing was created; a 5xx, a lost connection or an unreadable response
// leaves it open.
func cloneOutcomeUncertain(err error) bool {
	var apiErr *client.APIError
	if errors.As(err, &apiErr) {
		return apiErr.StatusCode >= http.StatusInternalServerError
	}

	return true
}

// findClone looks up the clone of the source container that carries tagFilter.
// Aikido allows one image per tag among a source and its clones in a registry,
// so a single match is the clone; anything else is left for the user to settle.
func (r *containerCloneResource) findClone(ctx context.Context, sourceID int64, tagFilter string) (int64, error) {
	source, err := containers.ByID(ctx, r.client, sourceID)
	if err != nil {
		return 0, err
	}
	all, err := containers.All(ctx, r.client)
	if err != nil {
		return 0, err
	}

	var matches []int64
	for _, candidate := range all {
		if isCloneOf(candidate, source) && candidate.TagFilter == tagFilter {
			matches = append(matches, candidate.ID)
		}
	}
	if len(matches) != 1 {
		return 0, fmt.Errorf("found %d clones of container %d with tag filter %q, want exactly 1",
			len(matches), sourceID, tagFilter)
	}

	return matches[0], nil
}

// verifyCloneOfSource guards an import against a mistyped pair, which would
// otherwise only surface as a misplaced clone on the next replacement.
func (r *containerCloneResource) verifyCloneOfSource(ctx context.Context, sourceID, cloneID int64) error {
	source, err := containers.ByID(ctx, r.client, sourceID)
	if err != nil {
		return err
	}
	clone, err := containers.ByID(ctx, r.client, cloneID)
	if err != nil {
		return err
	}
	if !isCloneOf(clone, source) {
		return fmt.Errorf("container %d (%s) is not a clone of container %d (%s) in the same registry",
			cloneID, clone.Name, sourceID, source.Name)
	}

	return nil
}

// isCloneOf reports whether candidate is another image of the same repository in
// the same registry as source. A clone carries its source's name and registry.
func isCloneOf(candidate, source containers.Container) bool {
	return candidate.ID != source.ID &&
		candidate.Name == source.Name &&
		sameRegistry(candidate.RegistryID, source.RegistryID)
}

func sameRegistry(left, right *int64) bool {
	return left != nil && right != nil && *left == *right
}

// parseContainerCloneImportID splits <source_container_id>:<clone_id>.
func parseContainerCloneImportID(importID string) (int64, int64, error) {
	parts := strings.Split(importID, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("import ID %q must be <source_container_id>:<clone_id>", importID)
	}

	sourceID, err := parseContainerID(parts[0])
	if err != nil {
		return 0, 0, err
	}
	cloneID, err := parseContainerID(parts[1])
	if err != nil {
		return 0, 0, err
	}
	if sourceID < 1 || cloneID < 1 {
		return 0, 0, fmt.Errorf("import ID %q must hold two positive container IDs", importID)
	}

	return sourceID, cloneID, nil
}

// cloneContainer asks Aikido for a clone of the source container and returns the
// clone's ID. The shared list is dropped either way: a failed call may still have
// created the clone, and a successful one added a container the list lacks.
func (r *containerCloneResource) cloneContainer(ctx context.Context, sourceID int64, tagFilter string) (int64, error) {
	defer containers.InvalidateCache(r.client)

	body := struct {
		ContainerRepoID int64  `json:"container_repo_id"`
		TagFilter       string `json:"tag_filter"`
	}{ContainerRepoID: sourceID, TagFilter: tagFilter}

	var cloned struct {
		ContainerRepoID int64 `json:"container_repo_id"`
	}
	if err := r.client.Do(ctx, http.MethodPost, containers.BasePath+"/clone", body, &cloned); err != nil {
		return 0, err
	}
	if cloned.ContainerRepoID <= 0 {
		return 0, fmt.Errorf("aikido reported no ID for the clone of container %d", sourceID)
	}

	return cloned.ContainerRepoID, nil
}

func (r *containerCloneResource) deactivateClone(ctx context.Context, id int64) error {
	defer containers.InvalidateCache(r.client)

	body := map[string]int64{"container_repo_id": id}

	return r.client.Do(ctx, http.MethodPost, containers.BasePath+"/deactivate", body, nil)
}

// containerCloneReadState refreshes the tag filter, so a filter changed in Aikido
// shows up as drift. The source is carried over from state, where Create
// recorded it.
func containerCloneReadState(apiContainer containers.Container, priorState containerCloneModel) containerCloneModel {
	return containerCloneModel{
		ID:                types.StringValue(strconv.FormatInt(apiContainer.ID, 10)),
		SourceContainerID: priorState.SourceContainerID,
		TagFilter:         types.StringValue(apiContainer.TagFilter),
	}
}
