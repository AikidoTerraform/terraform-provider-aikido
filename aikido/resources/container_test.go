package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

func TestContainerSchema_IsValid(t *testing.T) {
	ctx := context.Background()
	response := &resource.SchemaResponse{}

	NewContainerResource().Schema(ctx, resource.SchemaRequest{}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", response.Diagnostics)
	}

	if diagnostics := response.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatalf("schema implementation: %v", diagnostics)
	}
}

func TestContainerMetadata_TypeName(t *testing.T) {
	response := &resource.MetadataResponse{}

	NewContainerResource().Metadata(
		context.Background(),
		resource.MetadataRequest{ProviderTypeName: "aikido"},
		response,
	)

	if response.TypeName != "aikido_container" {
		t.Errorf("TypeName = %q, want aikido_container", response.TypeName)
	}
}

// Attributes the resource writes must be present and configurable; attributes the
// API only reports must not be.
func TestContainerSchema_AttributeModes(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewContainerResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	tests := []struct {
		name     string
		required bool
		optional bool
		computed bool
	}{
		{name: "id", required: true},
		{name: "active", required: true},
		{name: "tag_filter", optional: true},
		{name: "sensitivity", optional: true, computed: true},
		{name: "connectivity", optional: true, computed: true},
		{name: "labels", optional: true},
		{name: "linked_code_repo_id", optional: true, computed: true},
		{name: "name", computed: true},
		{name: "registry_provider", computed: true},
		{name: "registry_id", computed: true},
		{name: "registry_name", computed: true},
		{name: "cloud_id", computed: true},
		{name: "distro", computed: true},
		{name: "distro_version", computed: true},
		{name: "last_scanned_at", computed: true},
		{name: "last_scanned_tag", computed: true},
		{name: "last_scanned_digest", computed: true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attribute, ok := response.Schema.Attributes[tt.name]
			if !ok {
				t.Fatalf("attribute %q missing from the schema", tt.name)
			}
			if attribute.IsRequired() != tt.required {
				t.Errorf("required = %v, want %v", attribute.IsRequired(), tt.required)
			}
			if attribute.IsOptional() != tt.optional {
				t.Errorf("optional = %v, want %v", attribute.IsOptional(), tt.optional)
			}
			if attribute.IsComputed() != tt.computed {
				t.Errorf("computed = %v, want %v", attribute.IsComputed(), tt.computed)
			}
		})
	}
}

// tag_filter is Optional without Computed on purpose: null means newest-image and
// must stay writable. An empty string would mean the same thing, so only null may
// express it.
func TestContainerSchema_TagFilterRejectsTheEmptyString(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewContainerResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	attribute, ok := response.Schema.Attributes["tag_filter"]
	if !ok {
		t.Fatal("tag_filter missing from the schema")
	}
	if attribute.IsComputed() {
		t.Error("tag_filter must not be Computed: a null config value has to reach the API")
	}

	stringAttribute, ok := attribute.(schema.StringAttribute)
	if !ok {
		t.Fatalf("tag_filter is %T, want schema.StringAttribute", attribute)
	}
	if len(stringAttribute.Validators) == 0 {
		t.Error("tag_filter needs a validator rejecting the empty string")
	}
}

// containerAPI records what the write path sent and serves plausible responses.
type containerAPI struct {
	activated    []int64
	deactivated  []int64
	tagFilters   []json.RawMessage
	sensitivity  []string
	connectivity []string
	linkedRepos  []int64
	detail       containers.Container
}

func (api *containerAPI) server(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/containers/activate":
			var body struct {
				ContainerRepoID int64 `json:"container_repo_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.activated = append(api.activated, body.ContainerRepoID)
			_, _ = io.WriteString(w, `{"success":1,"was_already_activated":0}`)

		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/containers/deactivate":
			var body struct {
				ContainerRepoID int64 `json:"container_repo_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.deactivated = append(api.deactivated, body.ContainerRepoID)
			_, _ = io.WriteString(w, `{"success":1,"was_already_deactivated":0}`)

		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/containers/updateTagFilter":
			var body struct {
				ContainerRepoID int64           `json:"container_repo_id"`
				TagFilter       json.RawMessage `json:"tag_filter"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.tagFilters = append(api.tagFilters, body.TagFilter)
			_, _ = io.WriteString(w, `{"success":1}`)

		case r.Method == http.MethodPut && r.URL.Path == "/public/v1/containers/1/sensitivity":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.sensitivity = append(api.sensitivity, body["sensitivity"])
			_, _ = io.WriteString(w, `{"success":true}`)

		case r.Method == http.MethodPut && r.URL.Path == "/public/v1/containers/1/internetConnection":
			var body map[string]string
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.connectivity = append(api.connectivity, body["internet_exposed"])
			_, _ = io.WriteString(w, `{"success":true}`)

		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/containers/linkCodeRepo":
			var body struct {
				CodeRepoID int64 `json:"code_repo_id"`
			}
			_ = json.NewDecoder(r.Body).Decode(&body)
			api.linkedRepos = append(api.linkedRepos, body.CodeRepoID)
			_, _ = io.WriteString(w, `{"success":1}`)

		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers/1":
			_ = json.NewEncoder(w).Encode(api.detail)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestSetContainerConfig_WritesEveryManagedAttribute(t *testing.T) {
	api := &containerAPI{detail: containers.Container{
		ID:       1,
		Name:     "pied-piper/compression",
		Provider: "aws",
		Active:   true,
	}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:               types.StringValue("1"),
			Active:           types.BoolValue(true),
			TagFilter:        types.StringValue("prod-*"),
			Sensitivity:      types.StringValue("sensitive"),
			Connectivity:     types.StringValue("connected"),
			LinkedCodeRepoID: types.Int64Value(67),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.activated) != 1 || api.activated[0] != 1 {
		t.Errorf("activated = %v, want [1]", api.activated)
	}
	if len(api.deactivated) != 0 {
		t.Errorf("deactivated = %v, want none", api.deactivated)
	}
	if len(api.tagFilters) != 1 || string(api.tagFilters[0]) != `"prod-*"` {
		t.Errorf("tagFilters = %v, want [\"prod-*\"]", api.tagFilters)
	}
	if len(api.sensitivity) != 1 || api.sensitivity[0] != "sensitive" {
		t.Errorf("sensitivity = %v, want [sensitive]", api.sensitivity)
	}
	if len(api.connectivity) != 1 || api.connectivity[0] != "connected" {
		t.Errorf("connectivity = %v, want [connected]", api.connectivity)
	}
	if len(api.linkedRepos) != 1 || api.linkedRepos[0] != 67 {
		t.Errorf("linkedRepos = %v, want [67]", api.linkedRepos)
	}
	if state.Name.ValueString() != "pied-piper/compression" {
		t.Errorf("state.Name = %q", state.Name.ValueString())
	}
	if state.TagFilter.ValueString() != "prod-*" {
		t.Errorf("state.TagFilter = %q, want prod-*", state.TagFilter.ValueString())
	}
}

// Aikido rejects updateTagFilter on public Docker Hub images, on custom SBOM
// uploads, and when a sibling clone in the same region already scans this tag.
// Those guards fire on a no-op write too, so an omitted tag_filter over an
// already-empty filter must not call the endpoint at all.
func TestSetContainerConfig_SkipsTheTagFilterWriteWhenUnchanged(t *testing.T) {
	tests := []struct {
		name    string
		current string
		planned types.String
	}{
		{"both newest-image", "", types.StringNull()},
		{"same glob", "prod-*", types.StringValue("prod-*")},
		{"same special value", "semver-production", types.StringValue("semver-production")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &containerAPI{detail: containers.Container{ID: 1, Active: true, TagFilter: tt.current}}
			srv := api.server(t)

			_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
				context.Background(),
				containerModel{
					ID:        types.StringValue("1"),
					Active:    types.BoolValue(true),
					TagFilter: tt.planned,
				},
			)
			if err != nil {
				t.Fatalf("setContainerConfig: %v", err)
			}

			if len(api.tagFilters) != 0 {
				t.Errorf("tagFilters = %v, want no write", api.tagFilters)
			}
		})
	}
}

// Removing tag_filter from a container that has one is a real change: it resets
// the container to scanning the newest image, which the API expects as null.
func TestSetContainerConfig_ResetsTheTagFilterToNewestImage(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: true, TagFilter: "prod-*"}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.tagFilters) != 1 || string(api.tagFilters[0]) != "null" {
		t.Errorf("tagFilters = %v, want [null]", api.tagFilters)
	}
	if !state.TagFilter.IsNull() {
		t.Errorf("state.TagFilter = %v, want null", state.TagFilter)
	}
}

// sensitivity, connectivity and the code repo link are unmanaged when null, so a
// null config must send nothing at all for them.
func TestSetContainerConfig_SkipsUnmanagedAttributes(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: true}}
	srv := api.server(t)

	_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.sensitivity) != 0 {
		t.Errorf("sensitivity = %v, want none", api.sensitivity)
	}
	if len(api.connectivity) != 0 {
		t.Errorf("connectivity = %v, want none", api.connectivity)
	}
	if len(api.linkedRepos) != 0 {
		t.Errorf("linkedRepos = %v, want none", api.linkedRepos)
	}
}

// GET /containers/{id} does not return sensitivity or connectivity, so state
// takes the value that was just written.
func TestSetContainerConfig_StateFallsBackToThePlanWhenDetailOmitsFields(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: true}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:           types.StringValue("1"),
			Active:       types.BoolValue(true),
			Sensitivity:  types.StringValue("sensitive"),
			Connectivity: types.StringValue("connected"),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if state.Sensitivity.ValueString() != "sensitive" {
		t.Errorf("state.Sensitivity = %v, want sensitive", state.Sensitivity)
	}
	if state.Connectivity.ValueString() != "connected" {
		t.Errorf("state.Connectivity = %v, want connected", state.Connectivity)
	}
}

func TestSetContainerConfig_UnmanagedComputedAttributesBecomeNullNotUnknown(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: true}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(true),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	for name, value := range map[string]attr.Value{
		"sensitivity":         state.Sensitivity,
		"connectivity":        state.Connectivity,
		"linked_code_repo_id": state.LinkedCodeRepoID,
		"registry_id":         state.RegistryID,
		"cloud_id":            state.CloudID,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is unknown after apply; Terraform rejects that", name)
		}
		if !value.IsNull() {
			t.Errorf("%s = %v, want null", name, value)
		}
	}
}

func TestSetContainerConfig_DeactivatesWhenActiveIsFalse(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: false}}
	srv := api.server(t)

	_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:     types.StringValue("1"),
			Active: types.BoolValue(false),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.deactivated) != 1 || api.deactivated[0] != 1 {
		t.Errorf("deactivated = %v, want [1]", api.deactivated)
	}
	if len(api.activated) != 0 {
		t.Errorf("activated = %v, want none", api.activated)
	}
}

// Activation comes first so that the remaining writes land on an active
// container, and the detail read comes before the tag filter write so the write
// can be skipped when nothing changed.
func TestSetContainerConfig_ActivatesThenReadsThenWrites(t *testing.T) {
	var order []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		order = append(order, r.Method+" "+r.URL.Path)

		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode(containers.Container{ID: 1, Active: true})
			return
		}
		_, _ = io.WriteString(w, `{"success":1}`)
	}))
	t.Cleanup(srv.Close)

	_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:          types.StringValue("1"),
			Active:      types.BoolValue(true),
			TagFilter:   types.StringValue("prod-*"),
			Sensitivity: types.StringValue("sensitive"),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(order) < 3 {
		t.Fatalf("calls = %v, want at least activate, detail, tag filter", order)
	}
	if !strings.HasSuffix(order[0], "/containers/activate") {
		t.Errorf("first call = %q, want the activate endpoint", order[0])
	}
	if order[1] != "GET /public/v1/containers/1" {
		t.Errorf("second call = %q, want the detail read", order[1])
	}
	if order[2] != "POST /public/v1/containers/updateTagFilter" {
		t.Errorf("third call = %q, want the tag filter write", order[2])
	}
}

func TestParseContainerID_RejectsNonNumeric(t *testing.T) {
	if _, err := parseContainerID("abc"); err == nil {
		t.Error("parseContainerID(\"abc\"): want an error, got nil")
	}

	id, err := parseContainerID("42")
	if err != nil {
		t.Fatalf("parseContainerID: %v", err)
	}
	if id != 42 {
		t.Errorf("id = %d, want 42", id)
	}
}

func containerListServer(t *testing.T, items ...containers.Container) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers" {
			_ = json.NewEncoder(w).Encode(items)
			return
		}
		t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
		w.WriteHeader(http.StatusNotFound)
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestContainerReadState_LoadsEveryAttributeFromTheList(t *testing.T) {
	srv := containerListServer(t, containers.Container{
		ID:           1,
		Name:         "pied-piper/compression",
		Provider:     "aws",
		RegistryName: "111222333444",
		TagFilter:    "prod-*",
		Sensitivity:  "sensitive",
		Connectivity: "connected",
		Active:       true,
	})

	apiContainer, err := containers.ByID(context.Background(), testClient(srv), 1)
	if err != nil {
		t.Fatalf("ByID: %v", err)
	}

	state := containerReadState(apiContainer, labelSet("production"))
	if state.ID.ValueString() != "1" {
		t.Errorf("ID = %q, want 1", state.ID.ValueString())
	}
	if state.TagFilter.ValueString() != "prod-*" {
		t.Errorf("TagFilter = %q, want prod-*", state.TagFilter.ValueString())
	}
	if state.RegistryName.ValueString() != "111222333444" {
		t.Errorf("RegistryName = %q", state.RegistryName.ValueString())
	}
	if state.RegistryProvider.ValueString() != "aws" {
		t.Errorf("RegistryProvider = %q, want aws", state.RegistryProvider.ValueString())
	}
	if state.Sensitivity.ValueString() != "sensitive" {
		t.Errorf("Sensitivity = %q, want sensitive", state.Sensitivity.ValueString())
	}
	if !state.Active.ValueBool() {
		t.Error("Active = false, want true")
	}
}

// Labels omitted from the configuration are unmanaged, so a refresh must not
// import the labels Aikido happens to hold — that would show them as drift and
// then delete them on the next apply.
func TestContainerReadState_NilPriorLabelsStayUnmanaged(t *testing.T) {
	apiContainer := containers.Container{
		ID:     1,
		Active: true,
		Labels: []containers.Label{{ID: "10", Name: "production"}},
	}

	t.Run("nil prior labels stay nil", func(t *testing.T) {
		state := containerReadState(apiContainer, nil)

		if state.Labels != nil {
			t.Errorf("Labels = %#v, want nil", state.Labels)
		}
	})

	t.Run("managed labels are read back from the API", func(t *testing.T) {
		state := containerReadState(apiContainer, labelSet("production"))

		if len(state.Labels) != 1 || state.Labels[0].ValueString() != "production" {
			t.Errorf("Labels = %#v, want [production]", state.Labels)
		}
	})

	t.Run("an empty managed set is not nil", func(t *testing.T) {
		state := containerReadState(containers.Container{ID: 1}, []types.String{})

		if state.Labels == nil {
			t.Error("Labels = nil, want empty non-nil: a managed empty set differs from omitted")
		}
	})
}

// A container absent from a list the API returned successfully is gone. A failed
// request is not proof of that and must never remove the resource from state.
func TestContainerRead_DistinguishesAbsentFromFailed(t *testing.T) {
	t.Run("absent is not-in-list", func(t *testing.T) {
		srv := containerListServer(t, containers.Container{ID: 1})

		_, err := containers.ByID(context.Background(), testClient(srv), 999)
		if !client.NotInList(err) {
			t.Errorf("err = %v, want not-in-list", err)
		}
	})

	t.Run("a failed request is not not-in-list", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusInternalServerError)
			_, _ = io.WriteString(w, `{"error":"boom"}`)
		}))
		t.Cleanup(srv.Close)

		_, err := containers.ByID(context.Background(), testClient(srv), 1)
		if err == nil {
			t.Fatal("want an error")
		}
		if client.NotInList(err) {
			t.Errorf("err = %v, must not read as absent", err)
		}
	})
}

func TestContainerDelete_DeactivatesAndToleratesA404(t *testing.T) {
	t.Run("deactivates", func(t *testing.T) {
		var method, path string
		var body map[string]int64

		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method, path = r.Method, r.URL.Path
			defer r.Body.Close()
			_ = json.NewDecoder(r.Body).Decode(&body)
			_, _ = io.WriteString(w, `{"success":1,"was_already_deactivated":0}`)
		}))
		t.Cleanup(srv.Close)

		res := &containerResource{client: testClient(srv)}
		if err := res.setActive(context.Background(), 42, false); err != nil {
			t.Fatalf("setActive: %v", err)
		}

		if method != http.MethodPost || path != "/public/v1/containers/deactivate" {
			t.Errorf("%s %s, want POST .../containers/deactivate", method, path)
		}
		if body["container_repo_id"] != 42 {
			t.Errorf("body = %#v, want container_repo_id 42", body)
		}
	})

	t.Run("a 404 is tolerated", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"no such container"}`)
		}))
		t.Cleanup(srv.Close)

		err := (&containerResource{client: testClient(srv)}).setActive(context.Background(), 42, false)
		if !client.NotFound(err) {
			t.Errorf("err = %v, want an API 404 the caller can tolerate", err)
		}
	})
}

// Aikido refuses to deactivate public images and self-managed SBOM uploads, and
// no field in the response predicts it, so destroy fails with the reason but no
// way forward. The remedy belongs in the diagnostic.
func TestDeactivationFailureDetail(t *testing.T) {
	const refusal = "You can not deactivate public images or self managed SBOM repos, delete these instead."

	t.Run("a 400 carries the remedy", func(t *testing.T) {
		detail := deactivationFailureDetail(&client.APIError{
			StatusCode: http.StatusBadRequest,
			Method:     http.MethodPost,
			Path:       "/public/v1/containers/deactivate",
			Body:       refusal,
		})

		if !strings.Contains(detail, refusal) {
			t.Errorf("detail dropped the API reason: %q", detail)
		}
		if !strings.Contains(detail, "terraform state rm") {
			t.Errorf("detail names no remedy: %q", detail)
		}
	})

	t.Run("another failure is passed through", func(t *testing.T) {
		detail := deactivationFailureDetail(&client.APIError{
			StatusCode: http.StatusInternalServerError,
			Body:       "boom",
		})

		if !strings.Contains(detail, "boom") {
			t.Errorf("detail dropped the API reason: %q", detail)
		}
		if strings.Contains(detail, "terraform state rm") {
			t.Errorf("a server error must not be reported as a refusal: %q", detail)
		}
	})
}

// An Optional+Computed attribute omitted from the configuration reaches the
// provider as unknown, not null — that is how Terraform asks the provider to
// fill it. Unknown must not be written to the API, and must not survive into
// state, which Terraform rejects after apply.
func TestSetContainerConfig_OmittedComputedAttributesArriveUnknown(t *testing.T) {
	api := &containerAPI{detail: containers.Container{ID: 1, Active: true}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:               types.StringValue("1"),
			Active:           types.BoolValue(true),
			TagFilter:        types.StringNull(),
			Sensitivity:      types.StringUnknown(),
			Connectivity:     types.StringUnknown(),
			LinkedCodeRepoID: types.Int64Unknown(),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.sensitivity) != 0 || len(api.connectivity) != 0 || len(api.linkedRepos) != 0 {
		t.Errorf("unknown attributes were written: sensitivity=%v connectivity=%v linked=%v",
			api.sensitivity, api.connectivity, api.linkedRepos)
	}

	for name, value := range map[string]attr.Value{
		"sensitivity":         state.Sensitivity,
		"connectivity":        state.Connectivity,
		"linked_code_repo_id": state.LinkedCodeRepoID,
	} {
		if value.IsUnknown() {
			t.Errorf("%s is still unknown after apply; Terraform rejects that", name)
		}
	}
}

// An unknown tag_filter must not be read as newest-image: that would reset a
// real filter. tag_filter is Optional without Computed, so this should not occur
// at apply time, but the comparison must not silently treat it as empty.
func TestTagFilterChanged_TreatsUnknownAsNoChange(t *testing.T) {
	tests := []struct {
		name    string
		planned types.String
		current string
		want    bool
	}{
		{"unknown over an existing filter", types.StringUnknown(), "prod-*", false},
		{"unknown over an empty filter", types.StringUnknown(), "", false},
		{"null over an existing filter", types.StringNull(), "prod-*", true},
		{"null over an empty filter", types.StringNull(), "", false},
		{"new value", types.StringValue("prod-*"), "", true},
		{"same value", types.StringValue("prod-*"), "prod-*", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tagFilterChanged(tt.planned, tt.current); got != tt.want {
				t.Errorf("tagFilterChanged(%v, %q) = %v, want %v", tt.planned, tt.current, got, tt.want)
			}
		})
	}
}

func TestFirstKnownString(t *testing.T) {
	tests := []struct {
		name    string
		fromAPI types.String
		planned types.String
		want    types.String
	}{
		{
			name:    "the API value wins over the plan",
			fromAPI: types.StringValue("sensitive"),
			planned: types.StringValue("normal"),
			want:    types.StringValue("sensitive"),
		},
		{
			name:    "a null API value falls back to the plan",
			fromAPI: types.StringNull(),
			planned: types.StringValue("sensitive"),
			want:    types.StringValue("sensitive"),
		},
		{
			name:    "an unknown API value falls back to the plan",
			fromAPI: types.StringUnknown(),
			planned: types.StringValue("sensitive"),
			want:    types.StringValue("sensitive"),
		},
		{
			name:    "an unmanaged attribute becomes null, never unknown",
			fromAPI: types.StringNull(),
			planned: types.StringUnknown(),
			want:    types.StringNull(),
		},
		{
			name:    "both absent becomes null",
			fromAPI: types.StringNull(),
			planned: types.StringNull(),
			want:    types.StringNull(),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := firstKnownString(tt.fromAPI, tt.planned)
			if !got.Equal(tt.want) {
				t.Errorf("firstKnownString(%v, %v) = %v, want %v", tt.fromAPI, tt.planned, got, tt.want)
			}
		})
	}
}

// firstKnownString relies on never receiving a known-empty API value, which
// nullIfEmptyString guarantees for every string containerModelFromAPI maps.
func TestContainerModelFromAPI_EmptyStringsBecomeNull(t *testing.T) {
	model := containerModelFromAPI(containers.Container{ID: 1})

	for name, value := range map[string]types.String{
		"sensitivity":         model.Sensitivity,
		"connectivity":        model.Connectivity,
		"tag_filter":          model.TagFilter,
		"registry_name":       model.RegistryName,
		"distro":              model.Distro,
		"distro_version":      model.DistroVersion,
		"last_scanned_tag":    model.LastScannedTag,
		"last_scanned_digest": model.LastScannedDigest,
	} {
		if !value.IsNull() {
			t.Errorf("%s = %v, want null for an empty API string", name, value)
		}
	}
}
