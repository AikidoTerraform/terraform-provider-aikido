package datasources

import (
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/labels"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func ptr[T any](v T) *T {
	return &v
}

func ecrContainer() containers.Container {
	return containers.Container{
		ID:           1,
		Name:         "pied-piper/compression",
		Provider:     "aws",
		RegistryID:   ptr(int64(3)),
		RegistryName: "111222333444",
		TagFilter:    "prod-*",
		Active:       true,
		Labels: []labels.Label{
			{Name: "production"},
			{Name: "tier:1"},
		},
	}
}

func TestContainerMatchesFilters(t *testing.T) {
	container := ecrContainer()

	tests := []struct {
		name   string
		config containersDataSourceModel
		labels []string
		want   bool
	}{
		{
			name:   "no filters matches everything",
			config: containersDataSourceModel{},
			want:   true,
		},
		{
			name:   "name matches exactly",
			config: containersDataSourceModel{Name: types.StringValue("pied-piper/compression")},
			want:   true,
		},
		{
			name:   "name is not a substring match",
			config: containersDataSourceModel{Name: types.StringValue("compression")},
			want:   false,
		},
		{
			name:   "tag_filter matches",
			config: containersDataSourceModel{TagFilter: types.StringValue("prod-*")},
			want:   true,
		},
		{
			name:   "a different tag_filter excludes",
			config: containersDataSourceModel{TagFilter: types.StringValue("dev-*")},
			want:   false,
		},
		{
			name:   "tag_filter is not a substring match",
			config: containersDataSourceModel{TagFilter: types.StringValue("prod")},
			want:   false,
		},
		{
			name:   "registry_name matches",
			config: containersDataSourceModel{RegistryName: types.StringValue("111222333444")},
			want:   true,
		},
		{
			name:   "a different registry_name excludes",
			config: containersDataSourceModel{RegistryName: types.StringValue("999888777666")},
			want:   false,
		},
		{
			name:   "registry_id matches",
			config: containersDataSourceModel{RegistryID: types.Int64Value(3)},
			want:   true,
		},
		{
			name:   "registry_provider matches",
			config: containersDataSourceModel{RegistryProvider: types.StringValue("aws")},
			want:   true,
		},
		{
			name:   "cloud_id excludes a registry-sourced container",
			config: containersDataSourceModel{CloudID: types.Int64Value(9)},
			want:   false,
		},
		{
			name:   "active false excludes an active container",
			config: containersDataSourceModel{Active: types.BoolValue(false)},
			want:   false,
		},
		{
			name: "filters combine with AND",
			config: containersDataSourceModel{
				Name:         types.StringValue("pied-piper/compression"),
				RegistryName: types.StringValue("999888777666"),
			},
			want: false,
		},
		{
			name:   "empty label filter matches everything",
			labels: []string{},
			want:   true,
		},
		{
			name:   "all labels must be present",
			labels: []string{"production", "tier:1"},
			want:   true,
		},
		{
			name:   "a missing label excludes",
			labels: []string{"production", "tier:0"},
			want:   false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := containerMatchesFilters(container, tt.config, tt.labels); got != tt.want {
				t.Errorf("containerMatchesFilters = %v, want %v", got, tt.want)
			}
		})
	}
}

// The same repository name can exist in two registries. registry_name is the
// discriminator, and without it both must come back so that one(...) fails in
// Terraform rather than the provider picking one.
func TestMatchingContainers_DisambiguatesIdenticalNamesByRegistry(t *testing.T) {
	first := ecrContainer()
	second := ecrContainer()
	second.ID = 2
	second.RegistryID = ptr(int64(4))
	second.RegistryName = "999888777666"

	all := []containers.Container{first, second}

	t.Run("name alone matches both", func(t *testing.T) {
		config := containersDataSourceModel{Name: types.StringValue("pied-piper/compression")}

		matched, ids := matchingContainers(all, config, nil)
		if len(matched) != 2 || len(ids) != 2 {
			t.Fatalf("matched %d containers and %d ids, want 2 and 2", len(matched), len(ids))
		}
	})

	t.Run("name plus registry_name matches one", func(t *testing.T) {
		config := containersDataSourceModel{
			Name:         types.StringValue("pied-piper/compression"),
			RegistryName: types.StringValue("999888777666"),
		}

		matched, ids := matchingContainers(all, config, nil)
		if len(matched) != 1 || len(ids) != 1 {
			t.Fatalf("matched %d containers and %d ids, want 1 and 1", len(matched), len(ids))
		}
		if ids[0].ValueInt64() != 2 {
			t.Errorf("id = %d, want 2", ids[0].ValueInt64())
		}
	})
}

// An empty tag_filter selects the containers scanning their newest image, which
// is the same meaning the attribute carries on aikido_container.
func TestMatchingContainers_EmptyTagFilterSelectsNewestImageContainers(t *testing.T) {
	all := []containers.Container{
		{ID: 1, Name: "filtered", TagFilter: "prod-*"},
		{ID: 2, Name: "newest-image", TagFilter: ""},
	}

	matched, ids := matchingContainers(all, containersDataSourceModel{TagFilter: types.StringValue("")}, nil)

	if len(matched) != 1 || len(ids) != 1 {
		t.Fatalf("matched %d containers and %d ids, want 1 and 1", len(matched), len(ids))
	}
	if ids[0].ValueInt64() != 2 {
		t.Errorf("id = %d, want 2", ids[0].ValueInt64())
	}
}

func TestMatchingContainers_IDsStayAlignedAfterFiltering(t *testing.T) {
	all := []containers.Container{
		{ID: 1, Name: "keep", Active: true},
		{ID: 2, Name: "drop", Active: false},
		{ID: 3, Name: "keep", Active: true},
	}

	matched, ids := matchingContainers(all, containersDataSourceModel{Active: types.BoolValue(true)}, nil)

	if len(matched) != len(ids) {
		t.Fatalf("%d containers but %d ids", len(matched), len(ids))
	}
	if len(matched) != 2 {
		t.Fatalf("matched %d containers, want 2", len(matched))
	}
	for i := range matched {
		if matched[i].ID.ValueString() != "1" && matched[i].ID.ValueString() != "3" {
			t.Errorf("position %d = id %s, want 1 or 3", i, matched[i].ID.ValueString())
		}
		if matched[i].ID.ValueString() == "1" && ids[i].ValueInt64() != 1 {
			t.Errorf("position %d: container id 1 paired with numeric id %d", i, ids[i].ValueInt64())
		}
		if matched[i].ID.ValueString() == "3" && ids[i].ValueInt64() != 3 {
			t.Errorf("position %d: container id 3 paired with numeric id %d", i, ids[i].ValueInt64())
		}
	}
}

func TestMatchingContainers_NoMatchYieldsEmptyNotNull(t *testing.T) {
	matched, ids := matchingContainers(
		[]containers.Container{{ID: 1, Name: "a"}},
		containersDataSourceModel{Name: types.StringValue("absent")},
		nil,
	)

	if matched == nil || len(matched) != 0 {
		t.Errorf("containers = %#v, want empty non-nil", matched)
	}
	if ids == nil || len(ids) != 0 {
		t.Errorf("ids = %#v, want empty non-nil", ids)
	}
}

// A cloud-sourced container carries no registry. Null must stay null in state:
// a 0 would read as registry 0 and mis-scope a downstream filter.
func TestContainerModelFromAPI_NullIDsStayNull(t *testing.T) {
	model := containerModelFromAPI(containers.Container{ID: 1, CloudID: ptr(int64(9))})

	if !model.RegistryID.IsNull() {
		t.Errorf("RegistryID = %v, want null", model.RegistryID)
	}
	if model.CloudID.ValueInt64() != 9 {
		t.Errorf("CloudID = %v, want 9", model.CloudID)
	}
	if !model.LinkedCodeRepoID.IsNull() {
		t.Errorf("LinkedCodeRepoID = %v, want null", model.LinkedCodeRepoID)
	}
}

func TestUnknownContainerFilterDiagnostics(t *testing.T) {
	tests := []struct {
		name      string
		config    containersDataSourceModel
		wantError bool
	}{
		{
			name:   "known filters are accepted",
			config: containersDataSourceModel{Name: types.StringValue("a")},
		},
		{
			name:      "an unknown name is refused",
			config:    containersDataSourceModel{Name: types.StringUnknown()},
			wantError: true,
		},
		{
			name:      "an unknown registry_name is refused",
			config:    containersDataSourceModel{RegistryName: types.StringUnknown()},
			wantError: true,
		},
		{
			name:      "an unknown registry_id is refused",
			config:    containersDataSourceModel{RegistryID: types.Int64Unknown()},
			wantError: true,
		},
		{
			name:      "an unknown registry_provider is refused",
			config:    containersDataSourceModel{RegistryProvider: types.StringUnknown()},
			wantError: true,
		},
		{
			name:      "an unknown cloud_id is refused",
			config:    containersDataSourceModel{CloudID: types.Int64Unknown()},
			wantError: true,
		},
		{
			name:      "an unknown active is refused",
			config:    containersDataSourceModel{Active: types.BoolUnknown()},
			wantError: true,
		},
		{
			name:      "an unknown tag_filter is refused",
			config:    containersDataSourceModel{TagFilter: types.StringUnknown()},
			wantError: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			diagnostics := unknownContainerFilterDiagnostics(tt.config)
			if diagnostics.HasError() != tt.wantError {
				t.Errorf("HasError = %v, want %v (%v)", diagnostics.HasError(), tt.wantError, diagnostics)
			}
		})
	}
}
