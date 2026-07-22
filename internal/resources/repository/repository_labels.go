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
	byID   map[string]labelModel
	byName map[string]labelModel
}

func labelsSchemaAttribute() schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		Description: "Labels managed by this resource. Only listed labels are created/updated/removed; " +
			"pre-existing labels never managed here are left alone. Removing the last label from " +
			"config deletes it from Aikido.",
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

// syncLabels creates/updates/deletes only against prior Terraform state.
// Labels that exist in Aikido but were never in state are never deleted.
func (r *repositoryResource) syncLabels(ctx context.Context, repoID string, plan, prior []labelModel) ([]labelModel, error) {
	priorIdx := indexLabels(prior)
	out := make([]labelModel, 0, len(plan))
	kept := make(map[string]struct{}, len(plan))

	for i, planned := range plan {
		label, err := r.upsertLabel(ctx, repoID, i, planned, prior, priorIdx)
		if err != nil {
			return nil, err
		}
		kept[label.ID.ValueString()] = struct{}{}
		out = append(out, label)
	}

	if err := r.deleteRemovedLabels(ctx, repoID, priorIdx.byID, kept); err != nil {
		return nil, err
	}
	return out, nil
}

func indexLabels(labels []labelModel) labelIndex {
	idx := labelIndex{
		byID:   make(map[string]labelModel, len(labels)),
		byName: make(map[string]labelModel, len(labels)),
	}
	for _, l := range labels {
		if id := l.ID.ValueString(); id != "" {
			idx.byID[id] = l
		}
		if name := l.Name.ValueString(); name != "" {
			idx.byName[name] = l
		}
	}
	return idx
}

// upsertLabel resolves a planned label against prior state:
//  1. same name already managed → keep (handles list shrink/reorder)
//  2. known plan id still in prior → rename if needed
//  3. unknown id at same list index → treat as in-place rename
//  4. otherwise create
func (r *repositoryResource) upsertLabel(ctx context.Context, repoID string, index int, planned labelModel, prior []labelModel, priorIdx labelIndex) (labelModel, error) {
	name := planned.Name.ValueString()

	if existing, ok := priorIdx.byName[name]; ok && existing.ID.ValueString() != "" {
		return existing, nil
	}

	if id := knownLabelID(planned); id != "" {
		if p, ok := priorIdx.byID[id]; ok {
			return r.renameLabelIfNeeded(ctx, repoID, id, name, p)
		}
	}

	// Computed id is often unknown on update; reuse the prior label at this index.
	if index < len(prior) {
		if id := prior[index].ID.ValueString(); id != "" {
			return r.renameLabelIfNeeded(ctx, repoID, id, name, prior[index])
		}
	}

	return r.createLabel(ctx, repoID, name)
}

func (r *repositoryResource) renameLabelIfNeeded(ctx context.Context, repoID, id, name string, prior labelModel) (labelModel, error) {
	if prior.ID.ValueString() != "" && prior.Name.ValueString() != name {
		path := basePath + "/" + repoID + "/labels/" + id
		if err := r.client.Do(ctx, "POST", path, map[string]string{"name": name}, nil); err != nil {
			return labelModel{}, err
		}
	}
	imported := prior.ID.ValueString() != "" && prior.IsImported.ValueBool()
	return newLabel(id, name, imported), nil
}

func (r *repositoryResource) createLabel(ctx context.Context, repoID, name string) (labelModel, error) {
	var resp labelWriteResponse
	path := basePath + "/" + repoID + "/labels"
	if err := r.client.Do(ctx, "POST", path, map[string]string{"name": name}, &resp); err != nil {
		return labelModel{}, err
	}
	if resp.LabelID == 0 {
		return labelModel{}, fmt.Errorf("create label %q: empty label_id", name)
	}
	return newLabel(strconv.FormatInt(resp.LabelID, 10), name, false), nil
}

func (r *repositoryResource) deleteRemovedLabels(ctx context.Context, repoID string, priorByID map[string]labelModel, kept map[string]struct{}) error {
	for id, l := range priorByID {
		if _, ok := kept[id]; ok || l.IsImported.ValueBool() {
			continue
		}
		path := basePath + "/" + repoID + "/labels/" + id
		if err := r.client.Do(ctx, "DELETE", path, nil, nil); err != nil {
			return err
		}
	}
	return nil
}

func knownLabelID(l labelModel) string {
	if l.ID.IsNull() || l.ID.IsUnknown() {
		return ""
	}
	return l.ID.ValueString()
}

func newLabel(id, name string, imported bool) labelModel {
	return labelModel{
		ID:         types.StringValue(id),
		Name:       types.StringValue(name),
		IsImported: types.BoolValue(imported),
	}
}
