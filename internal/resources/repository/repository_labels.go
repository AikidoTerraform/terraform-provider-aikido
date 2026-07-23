package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/boolplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type labelAPI struct {
	ID         int64  `json:"id"`
	Name       string `json:"name"`
	IsImported bool   `json:"is_imported"`
}

type labelModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	IsImported types.Bool   `tfsdk:"is_imported"`
}

type labelWriteResponse struct {
	LabelID int64 `json:"label_id"`
}

func labelsSchemaAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		Description: "Labels managed by this resource. When set, Terraform creates/updates/deletes labels to match. " +
			"Omitting labels leaves Aikido labels untouched. An empty list deletes all labels currently on the repository.",
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Required:    true,
					Description: "Label name.",
				},
				"id": schema.StringAttribute{
					Computed:    true,
					Description: "Aikido label ID.",
					PlanModifiers: []planmodifier.String{
						stringplanmodifier.UseStateForUnknown(),
					},
				},
				"is_imported": schema.BoolAttribute{
					Computed:    true,
					Description: "True when the label was imported from the Git provider (read-only).",
					PlanModifiers: []planmodifier.Bool{
						boolplanmodifier.UseStateForUnknown(),
					},
				},
			},
		},
	}
}

// applyLabels makes Aikido match the planned labels.
// nil (omitted) is a no-op and leaves Aikido labels unchanged.
// An empty list fetches current labels from Aikido and deletes them.
// A non-empty list syncs against prior Terraform state (create/update/delete).
func (r *repositoryResource) applyLabels(ctx context.Context, repositoryID string, plannedLabels, priorLabels []labelModel) ([]labelModel, error) {
	if plannedLabels == nil {
		return nil, nil
	}

	// labels = [] means delete all labels
	if len(plannedLabels) == 0 {
		if err := r.deleteAllLabels(ctx, repositoryID); err != nil {
			return nil, err
		}
		return []labelModel{}, nil
	}

	synced := make([]labelModel, 0, len(plannedLabels))

	for _, planned := range plannedLabels {
		name := planned.Name.ValueString()
		id := planned.ID.ValueString()

		// existing id means update the label
		if id != "" {
			if existing, ok := labelByID(priorLabels, id); ok {
				if existing.Name.ValueString() != name {
					if err := r.updateLabel(ctx, repositoryID, id, name); err != nil {
						return nil, fmt.Errorf("updating label: %w", err)
					}
					existing.Name = types.StringValue(name)
				}
				synced = append(synced, existing)
				continue
			}
		}

		// create new label
		created, err := r.createLabel(ctx, repositoryID, name)
		if err != nil {
			return nil, fmt.Errorf("creating label: %w", err)
		}
		synced = append(synced, created)
	}

	// delete removed labels. Labels not existing in planned but existing in prior means they were deleted.
	for _, prior := range priorLabels {
		id := prior.ID.ValueString()
		if id == "" {
			continue
		}
		if _, ok := labelByID(plannedLabels, id); ok {
			continue
		}
		if err := r.deleteLabel(ctx, repositoryID, id); err != nil {
			return nil, fmt.Errorf("deleting label %q: %w", prior.Name.ValueString(), err)
		}
	}

	return synced, nil
}

func labelByID(labels []labelModel, id string) (labelModel, bool) {
	for _, label := range labels {
		if label.ID.ValueString() == id {
			return label, true
		}
	}
	return labelModel{}, false
}

// labelModelsFromAPI maps API labels into Terraform state models.
// Always returns a non-nil slice so managed empty differs from omitted (nil).
func labelModelsFromAPI(apiLabels []labelAPI) []labelModel {
	labels := make([]labelModel, 0, len(apiLabels))
	for _, apiLabel := range apiLabels {
		labels = append(labels, labelModel{
			ID:         types.StringValue(strconv.FormatInt(apiLabel.ID, 10)),
			Name:       types.StringValue(apiLabel.Name),
			IsImported: types.BoolValue(apiLabel.IsImported),
		})
	}

	return labels
}

func (r *repositoryResource) createLabel(ctx context.Context, repositoryID, labelName string) (labelModel, error) {
	var resp labelWriteResponse
	path := basePath + "/" + repositoryID + "/labels"

	if err := r.client.Do(ctx, "POST", path, map[string]string{"name": labelName}, &resp); err != nil {
		return labelModel{}, err
	}

	if resp.LabelID == 0 {
		return labelModel{}, fmt.Errorf("create label %q: empty label_id", labelName)
	}

	return labelModel{
		ID:   types.StringValue(strconv.FormatInt(resp.LabelID, 10)),
		Name: types.StringValue(labelName),
	}, nil
}

func (r *repositoryResource) updateLabel(ctx context.Context, repositoryID, labelID, labelName string) error {
	body := map[string]string{"name": labelName}
	path := basePath + "/" + repositoryID + "/labels/" + labelID

	if err := r.client.Do(ctx, "POST", path, body, nil); err != nil {
		return fmt.Errorf("update label %q: %w", labelName, err)
	}

	return nil
}

func (r *repositoryResource) deleteLabel(ctx context.Context, repositoryID, labelID string) error {
	path := basePath + "/" + repositoryID + "/labels/" + labelID

	if err := r.client.Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("delete label %q: %w", labelID, err)
	}

	return nil
}

func (r *repositoryResource) deleteAllLabels(ctx context.Context, repositoryID string) error {
	// fetch all labels from the repository. We cannot rely on the state.
	// When deleting the labels entirely, the state will be empty and no id's will be present to delete.
	details, err := r.getRepositoryDetails(ctx, repositoryID)
	if err != nil {
		return fmt.Errorf("reading labels: %w", err)
	}

	for _, label := range details.Labels {
		id := label.ID.ValueString()
		if id == "" {
			continue
		}
		if err := r.deleteLabel(ctx, repositoryID, id); err != nil {
			return fmt.Errorf("deleting label %q: %w", label.Name.ValueString(), err)
		}
	}
	return nil
}
