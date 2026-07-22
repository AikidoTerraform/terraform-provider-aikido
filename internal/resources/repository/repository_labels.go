package repository

import (
	"context"
	"fmt"
	"strconv"

	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// labelModel is one managed label. Name comes from config; id and is_imported are computed.
type labelModel struct {
	ID         types.String `tfsdk:"id"`
	Name       types.String `tfsdk:"name"`
	IsImported types.Bool   `tfsdk:"is_imported"`
}

type labelWriteResponse struct {
	LabelID int64 `json:"label_id"`
}

type labelIndex struct {
	byLabelID   map[string]labelModel
	byLabelName map[string]labelModel
}

func labelsSchemaAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		Description: "Labels managed by this resource. Only listed labels are created/updated/removed; " +
			"pre-existing labels never managed here are left alone. Removing labels from config or " +
			"setting an empty list deletes previously managed labels and clears them from state.",
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

// applyLabels syncs managed labels to the API and returns the value to store in
// Terraform state. Sync runs when config lists labels (including empty) or when
// config omits them but prior state still had managed ones. Only when both plan
// and prior are empty do we leave Aikido labels untouched.
//
// State: omitted attribute → nil; empty list → empty non-nil slice (avoids null↔[] drift).
func (r *repositoryResource) applyLabels(ctx context.Context, repositoryID string, plannedLabels, priorLabels []labelModel) ([]labelModel, error) {
	if plannedLabels == nil && len(priorLabels) == 0 {
		return nil, nil
	}

	labelsToSync := plannedLabels
	if labelsToSync == nil {
		// Attribute removed from config (e.g. last label deleted) — treat as empty.
		labelsToSync = []labelModel{}
	}

	syncedLabels, err := r.syncLabels(ctx, repositoryID, labelsToSync, priorLabels)
	if err != nil {
		return nil, fmt.Errorf("updating labels: %w", err)
	}
	if plannedLabels == nil {
		return nil, nil
	}
	if syncedLabels == nil {
		return []labelModel{}, nil
	}
	return syncedLabels, nil
}

// syncLabels creates/updates/deletes only against prior Terraform state.
// Labels that exist in Aikido but were never in state are never deleted.
// An empty plan (attribute removed or labels = []) deletes all previously
// managed labels and returns a nil slice so Terraform state is reset.
func (r *repositoryResource) syncLabels(ctx context.Context, repositoryID string, plannedLabels, priorLabels []labelModel) ([]labelModel, error) {
	priorLabelIndex := indexLabels(priorLabels)

	// Removed from config or explicitly empty → drop managed labels from API + state.
	if len(plannedLabels) == 0 {
		if err := r.deleteRemovedLabels(ctx, repositoryID, priorLabelIndex.byLabelID, nil); err != nil {
			return nil, err
		}
		return nil, nil
	}

	syncedLabels := make([]labelModel, 0, len(plannedLabels))
	keptLabelIDs := make(map[string]struct{}, len(plannedLabels))

	for planIndex, plannedLabel := range plannedLabels {
		label, err := r.upsertLabel(ctx, repositoryID, planIndex, plannedLabel, priorLabels, priorLabelIndex)
		if err != nil {
			return nil, err
		}
		keptLabelIDs[label.ID.ValueString()] = struct{}{}
		syncedLabels = append(syncedLabels, label)
	}

	if err := r.deleteRemovedLabels(ctx, repositoryID, priorLabelIndex.byLabelID, keptLabelIDs); err != nil {
		return nil, err
	}
	return syncedLabels, nil
}

func indexLabels(labels []labelModel) labelIndex {
	index := labelIndex{
		byLabelID:   make(map[string]labelModel, len(labels)),
		byLabelName: make(map[string]labelModel, len(labels)),
	}
	for _, label := range labels {
		if labelID := label.ID.ValueString(); labelID != "" {
			index.byLabelID[labelID] = label
		}
		if labelName := label.Name.ValueString(); labelName != "" {
			index.byLabelName[labelName] = label
		}
	}
	return index
}

// upsertLabel resolves a planned label against prior state:
//  1. same name already managed → keep (handles list shrink/reorder)
//  2. known plan id still in prior → rename if needed
//  3. unknown id at same list index → treat as in-place rename
//  4. otherwise create
func (r *repositoryResource) upsertLabel(ctx context.Context, repositoryID string, planIndex int, plannedLabel labelModel, priorLabels []labelModel, priorLabelIndex labelIndex) (labelModel, error) {
	labelName := plannedLabel.Name.ValueString()

	if existingLabel, found := priorLabelIndex.byLabelName[labelName]; found && existingLabel.ID.ValueString() != "" {
		return existingLabel, nil
	}

	if labelID := knownLabelID(plannedLabel); labelID != "" {
		if priorLabel, found := priorLabelIndex.byLabelID[labelID]; found {
			return r.renameLabelIfNeeded(ctx, repositoryID, labelID, labelName, priorLabel)
		}
	}

	// Computed id is often unknown on update; reuse the prior label at this index.
	if planIndex < len(priorLabels) {
		priorLabel := priorLabels[planIndex]
		if labelID := priorLabel.ID.ValueString(); labelID != "" {
			return r.renameLabelIfNeeded(ctx, repositoryID, labelID, labelName, priorLabel)
		}
	}

	return r.createLabel(ctx, repositoryID, labelName)
}

func (r *repositoryResource) renameLabelIfNeeded(ctx context.Context, repositoryID, labelID, labelName string, priorLabel labelModel) (labelModel, error) {
	if priorLabel.ID.ValueString() != "" && priorLabel.Name.ValueString() != labelName {
		path := basePath + "/" + repositoryID + "/labels/" + labelID
		if err := r.client.Do(ctx, "POST", path, map[string]string{"name": labelName}, nil); err != nil {
			return labelModel{}, err
		}
	}
	isImported := priorLabel.ID.ValueString() != "" && priorLabel.IsImported.ValueBool()
	return newLabel(labelID, labelName, isImported), nil
}

func (r *repositoryResource) createLabel(ctx context.Context, repositoryID, labelName string) (labelModel, error) {
	var writeResponse labelWriteResponse
	path := basePath + "/" + repositoryID + "/labels"
	if err := r.client.Do(ctx, "POST", path, map[string]string{"name": labelName}, &writeResponse); err != nil {
		return labelModel{}, err
	}
	if writeResponse.LabelID == 0 {
		return labelModel{}, fmt.Errorf("create label %q: empty label_id", labelName)
	}
	return newLabel(strconv.FormatInt(writeResponse.LabelID, 10), labelName, false), nil
}

func (r *repositoryResource) deleteRemovedLabels(ctx context.Context, repositoryID string, priorLabelsByID map[string]labelModel, keptLabelIDs map[string]struct{}) error {
	for labelID, label := range priorLabelsByID {
		if _, stillKept := keptLabelIDs[labelID]; stillKept || label.IsImported.ValueBool() {
			continue
		}
		path := basePath + "/" + repositoryID + "/labels/" + labelID
		if err := r.client.Do(ctx, "DELETE", path, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func knownLabelID(label labelModel) string {
	if label.ID.IsNull() || label.ID.IsUnknown() {
		return ""
	}
	return label.ID.ValueString()
}

func newLabel(labelID, labelName string, isImported bool) labelModel {
	return labelModel{
		ID:         types.StringValue(labelID),
		Name:       types.StringValue(labelName),
		IsImported: types.BoolValue(isImported),
	}
}
