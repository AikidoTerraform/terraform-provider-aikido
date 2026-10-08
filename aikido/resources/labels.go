package resources

import (
	"context"
	"fmt"
	"slices"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/labels"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

type labelWriteResponse struct {
	LabelID int64 `json:"label_id"`
}

// labelsSchemaAttribute describes the managed label set. noun names the object the
// labels sit on.
func labelsSchemaAttribute(noun string) schema.SetAttribute {
	return schema.SetAttribute{
		Optional:    true,
		ElementType: types.StringType,
		Description: "Label names managed by this resource. " +
			"When set, Terraform creates/deletes labels to match. Omitting labels leaves Aikido labels untouched. " +
			"An empty set deletes every user-created label currently on the " + noun + ". " +
			"Labels imported from external sources are reported by data sources but cannot be managed here: " +
			"they are left unchanged, and naming one is rejected.",
	}
}

// applyLabels makes Aikido match the planned labels.
// no labels property in the terraform file means no labels are managed and existing labels in Aikido are left untouched.
// An empty list deletes every label currently on the object.
// a non empty list creates and deletes labels as needed to match the planned labels.
func applyLabels(
	ctx context.Context,
	apiClient *client.Client,
	basePath, id string,
	planned []types.String,
	current []labels.Label,
) error {
	if planned == nil {
		return nil
	}

	// Create planned names that don't exist yet.
	for _, label := range planned {
		name := label.ValueString()
		existing := slices.IndexFunc(current, func(l labels.Label) bool { return l.Name == name })
		if existing >= 0 {
			if current[existing].IsImported {
				return fmt.Errorf("label %q is imported and cannot be managed; remove it from the configuration", name)
			}

			continue
		}

		if err := createLabel(ctx, apiClient, basePath, id, name); err != nil {
			return fmt.Errorf("creating label %q: %w", name, err)
		}
	}

	// Delete existing labels that are no longer planned. Imported labels are never deleted.
	for _, label := range current {
		if label.IsImported {
			continue
		}

		if slices.ContainsFunc(planned, func(p types.String) bool { return p.ValueString() == label.Name }) {
			continue
		}

		// Without an ID the delete would hit the labels collection path.
		if label.ID == "" {
			return fmt.Errorf("deleting label %q: no id in API response", label.Name)
		}

		if err := deleteLabel(ctx, apiClient, basePath, id, label.ID); err != nil {
			return fmt.Errorf("deleting label %q: %w", label.Name, err)
		}
	}

	return nil
}

func createLabel(ctx context.Context, apiClient *client.Client, basePath, id, labelName string) error {
	var response labelWriteResponse
	path := basePath + "/" + id + "/labels"

	if err := apiClient.Do(ctx, "POST", path, map[string]string{"name": labelName}, &response); err != nil {
		return err
	}

	if response.LabelID == 0 {
		return fmt.Errorf("empty label_id in response")
	}

	return nil
}

func deleteLabel(ctx context.Context, apiClient *client.Client, basePath, id, labelID string) error {
	path := basePath + "/" + id + "/labels/" + labelID

	if err := apiClient.Do(ctx, "DELETE", path, nil, nil); err != nil {
		return fmt.Errorf("delete label %q: %w", labelID, err)
	}

	return nil
}
