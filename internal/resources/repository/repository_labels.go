package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// labelAPI is a label as returned by the repository detail endpoint.
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
		Optional:    true,
		Description: "Labels fully managed by this resource. Removing labels from config or setting an empty list deletes them.",
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"name": schema.StringAttribute{
					Required:    true,
					Description: "Label name.",
				},
				"id": schema.StringAttribute{
					Computed:    true,
					Description: "Aikido label ID.",
				},
				"is_imported": schema.BoolAttribute{
					Computed:    true,
					Description: "True when the label was imported from the Git provider (read-only).",
				},
			},
		},
	}
}

// applyLabels makes Aikido match the planned label names.
// Omitted labels (nil) with no prior state is a no-op; otherwise nil is treated as empty
// and clears managed labels from Aikido (state becomes nil again).
func (r *repositoryResource) applyLabels(ctx context.Context, repositoryID string, plannedLabels, priorLabels []labelModel) ([]labelModel, error) {
	if plannedLabels == nil && len(priorLabels) == 0 {
		return nil, nil
	}

	omitted := plannedLabels == nil
	if omitted {
		plannedLabels = []labelModel{}
	}

	result := make([]labelModel, 0, len(plannedLabels))
	for _, planned := range plannedLabels {
		name := planned.Name.ValueString()
		if priorLabel, found := findLabelByName(priorLabels, name); found {

			// update the label name when the label already existed and the name is different
			if err := r.updateLabel(ctx, repositoryID, priorLabel.ID.ValueString(), name); err != nil {
				return nil, fmt.Errorf("updating label: %w", err)
			}

			result = append(result, priorLabel)
			continue
		}

		// create the label when the label does not exist
		created, err := r.createLabel(ctx, repositoryID, name)
		if err != nil {
			return nil, fmt.Errorf("creating label: %w", err)
		}

		result = append(result, created)
	}

	for _, priorLabel := range priorLabels {
		if _, found := findLabelByName(plannedLabels, priorLabel.Name.ValueString()); found {
			// skip the label when it exists in the planned labels
			continue
		}

		if id := priorLabel.ID.ValueString(); id != "" {
			// delete the label when the label does not exist in the planned labels and it exists in the prior labels
			if err := r.deleteLabel(ctx, repositoryID, id); err != nil {
				return nil, fmt.Errorf("deleting label %q: %w", priorLabel.Name.ValueString(), err)
			}
		}
	}

	if omitted {
		return nil, nil
	}
	return result, nil
}

func findLabelByName(labels []labelModel, name string) (labelModel, bool) {
	found := false
	for _, label := range labels {
		if label.Name.ValueString() == name {
			found = true
			return label, found
		}
	}
	return labelModel{}, found
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

	fmt.Printf("created label %q\n", labelName)

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

	fmt.Printf("updated label %q\n", labelName)
	return nil
}

func (r *repositoryResource) deleteLabel(ctx context.Context, repositoryID, labelID string) error {
	path := basePath + "/" + repositoryID + "/labels/" + labelID

	if err := r.client.Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("delete label %q: %w", labelID, err)
	}

	fmt.Printf("deleted label %q\n", labelID)
	return nil
}
