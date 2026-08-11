package resources

import (
	"context"
	"fmt"
	"slices"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type labelAPI struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsImported bool   `json:"is_imported"`
}

type labelWriteResponse struct {
	LabelID int64 `json:"label_id"`
}

func labelsSchemaAttribute() schema.SetAttribute {
	return schema.SetAttribute{
		Optional:    true,
		ElementType: types.StringType,
		Description: "Label names managed by this resource. " +
			"When set, Terraform creates/deletes labels to match. Omitting labels leaves Aikido labels untouched. " +
			"An empty set deletes all labels currently on the repository.",
	}
}

// applyLabels makes Aikido match the planned labels.
// no labels property in the terraform file means no labels are managed and existing labels in Aikido are left untouched.
// An empty list deletes every label currently on the repository.
// a non empty list creates and deletes labels as needed to match the planned labels.
func (r *repositoryResource) applyLabels(ctx context.Context, repositoryID string, plannedLabels []types.String, currentLabels []labelAPI) error {
	if plannedLabels == nil {
		return nil
	}

	// Create planned names that don't exist yet.
	for _, label := range plannedLabels {
		name := label.ValueString()
		if slices.ContainsFunc(currentLabels, func(l labelAPI) bool { return l.Name == name }) {
			continue
		}

		if err := r.createLabel(ctx, repositoryID, name); err != nil {
			return fmt.Errorf("creating label %q: %w", name, err)
		}
	}

	// Delete existing labels that are no longer planned. Imported labels are never deleted.
	for _, label := range currentLabels {
		if label.IsImported {
			continue
		}

		if slices.ContainsFunc(plannedLabels, func(p types.String) bool { return p.ValueString() == label.Name }) {
			continue
		}

		if err := r.deleteLabel(ctx, repositoryID, label.ID); err != nil {
			return fmt.Errorf("deleting label %q: %w", label.Name, err)
		}
	}

	return nil
}

func (r *repositoryResource) createLabel(ctx context.Context, repositoryID, labelName string) error {
	var resp labelWriteResponse
	path := basePath + "/" + repositoryID + "/labels"

	if err := r.client.Do(ctx, "POST", path, map[string]string{"name": labelName}, &resp); err != nil {
		return err
	}

	if resp.LabelID == 0 {
		return fmt.Errorf("empty label_id in response")
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
