package resources

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/client"
	"github.com/AikidoTerraform/terraform-provider-aikido/internal/containers"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

func TestContainerCloneSchema_IsValid(t *testing.T) {
	ctx := context.Background()
	response := &resource.SchemaResponse{}

	NewContainerCloneResource().Schema(ctx, resource.SchemaRequest{}, response)

	if response.Diagnostics.HasError() {
		t.Fatalf("schema diagnostics: %v", response.Diagnostics)
	}

	if diagnostics := response.Schema.ValidateImplementation(ctx); diagnostics.HasError() {
		t.Fatalf("schema implementation: %v", diagnostics)
	}
}

func TestContainerCloneMetadata_TypeName(t *testing.T) {
	response := &resource.MetadataResponse{}

	NewContainerCloneResource().Metadata(
		context.Background(),
		resource.MetadataRequest{ProviderTypeName: "aikido"},
		response,
	)

	if response.TypeName != "aikido_container_clone" {
		t.Errorf("TypeName = %q, want aikido_container_clone", response.TypeName)
	}
}

// A clone is defined by its source and its tag filter, so changing either one
// means a different clone: every configurable attribute has to force a
// replacement.
func TestContainerCloneSchema_AttributeModes(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewContainerCloneResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	tests := []struct {
		name     string
		required bool
		computed bool
	}{
		{name: "id", computed: true},
		{name: "source_container_id", required: true},
		{name: "tag_filter", required: true},
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
			if attribute.IsOptional() {
				t.Error("optional = true, want false")
			}
			if attribute.IsComputed() != tt.computed {
				t.Errorf("computed = %v, want %v", attribute.IsComputed(), tt.computed)
			}
		})
	}
}

// Aikido trims the tag it stores, so a filter with surrounding whitespace would
// read back different from the configuration and replace the clone on every
// apply. Whitespace inside the filter is left to Aikido.
func TestContainerCloneSchema_TagFilterRejectsSurroundingWhitespace(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewContainerCloneResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	stringAttribute, ok := response.Schema.Attributes["tag_filter"].(schema.StringAttribute)
	if !ok {
		t.Fatalf("tag_filter is %T, want schema.StringAttribute", response.Schema.Attributes["tag_filter"])
	}

	tests := []struct {
		value     string
		wantError bool
	}{
		{"prod-*", false},
		{"p", false},
		{"semver-production", false},
		{"prod *", false},
		{"", true},
		{" ", true},
		{" prod-*", true},
		{"prod-* ", true},
		{"\tprod-*", true},
		{"prod-*\n", true},
	}

	for _, tt := range tests {
		t.Run(tt.value, func(t *testing.T) {
			validatorResponse := &validator.StringResponse{}
			for _, v := range stringAttribute.Validators {
				v.ValidateString(context.Background(), validator.StringRequest{
					Path:        path.Root("tag_filter"),
					ConfigValue: types.StringValue(tt.value),
				}, validatorResponse)
			}

			if validatorResponse.Diagnostics.HasError() != tt.wantError {
				t.Errorf("tag_filter %q: error = %v, want %v",
					tt.value, validatorResponse.Diagnostics.HasError(), tt.wantError)
			}
		})
	}
}

func TestCloneContainer_SendsTheSourceAndTagAndReturnsTheCloneID(t *testing.T) {
	var method, path string
	var body map[string]any

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		method, path = r.Method, r.URL.Path
		defer r.Body.Close()
		_ = json.NewDecoder(r.Body).Decode(&body)
		_, _ = io.WriteString(w, `{"container_repo_id":77}`)
	}))
	t.Cleanup(srv.Close)

	cloneID, err := (&containerCloneResource{client: testClient(srv)}).cloneContainer(context.Background(), 12, "prod-*")
	if err != nil {
		t.Fatalf("cloneContainer: %v", err)
	}

	if method != http.MethodPost || path != "/public/v1/containers/clone" {
		t.Errorf("%s %s, want POST .../containers/clone", method, path)
	}
	if body["container_repo_id"] != float64(12) || body["tag_filter"] != "prod-*" {
		t.Errorf("body = %#v, want container_repo_id 12 and tag_filter prod-*", body)
	}
	if cloneID != 77 {
		t.Errorf("cloneID = %d, want 77", cloneID)
	}
}

// Aikido explains every refusal itself: cloud-discovered containers, public
// images, SBOM uploads and a duplicate tag. That reason has to reach the user
// unchanged.
func TestCloneContainer_PassesAikidosRefusalThrough(t *testing.T) {
	const refusal = "A clone of this image already exists with the selected tag"

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = io.WriteString(w, refusal)
	}))
	t.Cleanup(srv.Close)

	_, err := (&containerCloneResource{client: testClient(srv)}).cloneContainer(context.Background(), 12, "prod-*")
	if !client.BadRequest(err) {
		t.Fatalf("err = %v, want the API's 400", err)
	}
	if !strings.Contains(err.Error(), refusal) {
		t.Errorf("err = %q, dropped the API reason", err.Error())
	}
}

// Storing ID 0 would point the resource at a container that does not exist, and
// the next refresh would silently drop it from state.
func TestCloneContainer_RejectsAResponseWithoutAnID(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, `{}`)
	}))
	t.Cleanup(srv.Close)

	if _, err := (&containerCloneResource{client: testClient(srv)}).cloneContainer(context.Background(), 12, "prod-*"); err == nil {
		t.Error("cloneContainer: want an error for a response without an ID")
	}
}

// The clone is a new container, so a read later in the same apply has to fetch
// a list that includes it.
func TestCloneContainer_DropsTheCachedList(t *testing.T) {
	var listCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers" {
			listCalls++
			_ = json.NewEncoder(w).Encode([]containers.Container{{ID: 12}})
			return
		}
		_, _ = io.WriteString(w, `{"container_repo_id":77}`)
	}))
	t.Cleanup(srv.Close)
	apiClient := testClient(srv)

	if _, err := containers.All(context.Background(), apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	if _, err := (&containerCloneResource{client: apiClient}).cloneContainer(context.Background(), 12, "prod-*"); err != nil {
		t.Fatalf("cloneContainer: %v", err)
	}
	if _, err := containers.All(context.Background(), apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}

	if listCalls != 2 {
		t.Errorf("list endpoint hit %d times, want 2: the cache must be dropped", listCalls)
	}
}

func TestDeactivateClone_DeactivatesAndReportsA404(t *testing.T) {
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

		if err := (&containerCloneResource{client: testClient(srv)}).deactivateClone(context.Background(), 77); err != nil {
			t.Fatalf("deactivateClone: %v", err)
		}

		if method != http.MethodPost || path != "/public/v1/containers/deactivate" {
			t.Errorf("%s %s, want POST .../containers/deactivate", method, path)
		}
		if body["container_repo_id"] != 77 {
			t.Errorf("body = %#v, want container_repo_id 77", body)
		}
	})

	t.Run("a 404 is one the caller can tolerate", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusNotFound)
			_, _ = io.WriteString(w, `{"error":"no such container"}`)
		}))
		t.Cleanup(srv.Close)

		err := (&containerCloneResource{client: testClient(srv)}).deactivateClone(context.Background(), 77)
		if !client.NotFound(err) {
			t.Errorf("err = %v, want an API 404 the caller can tolerate", err)
		}
	})
}

// cloneAPI answers the clone request with a fixed status and body, and serves
// current from the list endpoint, which is where a recovery looks the clone up.
type cloneAPI struct {
	cloneStatus int
	cloneBody   string
	current     []containers.Container
	listCalls   int
}

func (api *cloneAPI) server(t *testing.T) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		switch {
		case r.Method == http.MethodPost && r.URL.Path == "/public/v1/containers/clone":
			w.WriteHeader(api.cloneStatus)
			_, _ = io.WriteString(w, api.cloneBody)

		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers":
			api.listCalls++
			_ = json.NewEncoder(w).Encode(api.current)

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

// apiImage is a registry image as the list reports it. A source and its clones
// share a name and a registry.
func apiImage(id int64, name string, registryID int64, tagFilter string) containers.Container {
	return containers.Container{ID: id, Name: name, RegistryID: ptrTo(registryID), TagFilter: tagFilter}
}

func TestCloneOutcomeUncertain(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want bool
	}{
		{"a refusal", &client.APIError{StatusCode: http.StatusBadRequest}, false},
		{"a forbidden request", &client.APIError{StatusCode: http.StatusForbidden}, false},
		{"rate limited past every retry", &client.APIError{StatusCode: http.StatusTooManyRequests}, false},
		{"a server error", &client.APIError{StatusCode: http.StatusInternalServerError}, true},
		{"a gateway timeout", &client.APIError{StatusCode: http.StatusGatewayTimeout}, true},
		{"a lost connection", errors.New("performing request: connection reset by peer"), true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := cloneOutcomeUncertain(tt.err); got != tt.want {
				t.Errorf("cloneOutcomeUncertain(%v) = %v, want %v", tt.err, got, tt.want)
			}
		})
	}
}

func TestCreateClone_ReturnsTheClonedID(t *testing.T) {
	api := &cloneAPI{cloneStatus: http.StatusOK, cloneBody: `{"container_repo_id":77}`}
	srv := api.server(t)

	cloneID, diagnostics := (&containerCloneResource{client: testClient(srv)}).createClone(context.Background(), 12, "prod-*")
	if diagnostics.HasError() || diagnostics.WarningsCount() != 0 {
		t.Fatalf("diagnostics = %v, want none", diagnostics)
	}
	if cloneID != 77 {
		t.Errorf("cloneID = %d, want 77", cloneID)
	}
	if api.listCalls != 0 {
		t.Errorf("list endpoint hit %d times, want none after a clean clone", api.listCalls)
	}
}

// Aikido runs every check before saving, so a refusal created nothing, and an
// existing clone carrying the tag must not be adopted in its place.
func TestCreateClone_ARefusalIsNotRecovered(t *testing.T) {
	api := &cloneAPI{
		cloneStatus: http.StatusBadRequest,
		cloneBody:   "A clone of this image already exists with the selected tag",
		current: []containers.Container{
			apiImage(12, "pied-piper/api", 5, "prod-*"),
			apiImage(77, "pied-piper/api", 5, "staging-*"),
		},
	}
	srv := api.server(t)

	_, diagnostics := (&containerCloneResource{client: testClient(srv)}).createClone(context.Background(), 12, "staging-*")
	if !diagnostics.HasError() {
		t.Fatal("want the refusal as an error")
	}
	if api.listCalls != 0 {
		t.Errorf("list endpoint hit %d times, want no recovery lookup after a refusal", api.listCalls)
	}
}

// The clone can be saved and the response still lost. Recording the clone found
// under the source's name, registry and tag keeps it managed.
func TestCreateClone_RecoversTheCloneAfterAnUncertainResponse(t *testing.T) {
	tests := []struct {
		name        string
		cloneStatus int
		cloneBody   string
	}{
		{"a server error", http.StatusInternalServerError, "boom"},
		{"a response without an ID", http.StatusOK, `{}`},
		{"an unreadable response", http.StatusOK, `{"container_repo_id":`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &cloneAPI{
				cloneStatus: tt.cloneStatus,
				cloneBody:   tt.cloneBody,
				current: []containers.Container{
					apiImage(12, "pied-piper/api", 5, "staging-*"),
					apiImage(40, "pied-piper/api", 6, "staging-*"),
					apiImage(41, "pied-piper/web", 5, "staging-*"),
					apiImage(77, "pied-piper/api", 5, "staging-*"),
				},
			}
			srv := api.server(t)

			cloneID, diagnostics := (&containerCloneResource{client: testClient(srv)}).createClone(context.Background(), 12, "staging-*")
			if diagnostics.HasError() {
				t.Fatalf("diagnostics = %v, want a recovery", diagnostics)
			}
			if diagnostics.WarningsCount() != 1 {
				t.Errorf("warnings = %d, want 1 saying the clone was recovered", diagnostics.WarningsCount())
			}
			if cloneID != 77 {
				t.Errorf("cloneID = %d, want 77: not the source, nor another registry or repository", cloneID)
			}
		})
	}
}

// Without exactly one match the clone cannot be told apart, so the user gets the
// original failure and the import ID that adopts the clone.
func TestCreateClone_AnUnsettledRecoveryPointsAtImport(t *testing.T) {
	tests := []struct {
		name    string
		current []containers.Container
	}{
		{"no clone", []containers.Container{apiImage(12, "pied-piper/api", 5, "prod-*")}},
		{"two clones", []containers.Container{
			apiImage(12, "pied-piper/api", 5, "prod-*"),
			apiImage(77, "pied-piper/api", 5, "staging-*"),
			apiImage(78, "pied-piper/api", 5, "staging-*"),
		}},
		{"the source is gone", []containers.Container{apiImage(77, "pied-piper/api", 5, "staging-*")}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &cloneAPI{cloneStatus: http.StatusBadGateway, cloneBody: "upstream unavailable", current: tt.current}
			srv := api.server(t)

			_, diagnostics := (&containerCloneResource{client: testClient(srv)}).createClone(context.Background(), 12, "staging-*")
			if !diagnostics.HasError() {
				t.Fatal("want an error")
			}

			detail := diagnostics.Errors()[0].Detail()
			if !strings.Contains(detail, "upstream unavailable") {
				t.Errorf("detail dropped the original failure: %q", detail)
			}
			if !strings.Contains(detail, "terraform import") || !strings.Contains(detail, "12:<clone_id>") {
				t.Errorf("detail names no import ID: %q", detail)
			}
		})
	}
}

func TestParseContainerCloneImportID(t *testing.T) {
	sourceID, cloneID, err := parseContainerCloneImportID("12:77")
	if err != nil {
		t.Fatalf("parseContainerCloneImportID: %v", err)
	}
	if sourceID != 12 || cloneID != 77 {
		t.Errorf("got %d:%d, want 12:77", sourceID, cloneID)
	}

	for _, importID := range []string{"77", "12:77:1", "12:", ":77", "abc:77", "12:abc", "0:77", "12:-1"} {
		t.Run(importID, func(t *testing.T) {
			if _, _, err := parseContainerCloneImportID(importID); err == nil {
				t.Errorf("parseContainerCloneImportID(%q): want an error", importID)
			}
		})
	}
}

// Read fills the tag filter in after an import, so the import itself only has to
// record which clone it adopted and the source it was made from.
func TestContainerCloneImportState_RecordsTheCloneAndItsSource(t *testing.T) {
	ctx := context.Background()
	srv := containerListServer(t,
		apiImage(12, "pied-piper/api", 5, "prod-*"),
		apiImage(77, "pied-piper/api", 5, "staging-*"),
	)

	cloneResource := &containerCloneResource{client: testClient(srv)}
	schemaResponse := &resource.SchemaResponse{}
	cloneResource.Schema(ctx, resource.SchemaRequest{}, schemaResponse)

	response := &resource.ImportStateResponse{State: tfsdk.State{
		Schema: schemaResponse.Schema,
		Raw:    tftypes.NewValue(schemaResponse.Schema.Type().TerraformType(ctx), nil),
	}}
	cloneResource.ImportState(ctx, resource.ImportStateRequest{ID: "12:77"}, response)
	if response.Diagnostics.HasError() {
		t.Fatalf("ImportState: %v", response.Diagnostics)
	}

	var state containerCloneModel
	if diagnostics := response.State.Get(ctx, &state); diagnostics.HasError() {
		t.Fatalf("State.Get: %v", diagnostics)
	}
	if state.ID.ValueString() != "77" {
		t.Errorf("id = %v, want 77", state.ID)
	}
	if state.SourceContainerID.ValueInt64() != 12 {
		t.Errorf("source_container_id = %v, want 12", state.SourceContainerID)
	}
}

func TestVerifyCloneOfSource(t *testing.T) {
	srv := containerListServer(t,
		apiImage(12, "pied-piper/api", 5, "prod-*"),
		apiImage(77, "pied-piper/api", 5, "staging-*"),
		apiImage(40, "pied-piper/api", 6, "staging-*"),
		apiImage(41, "pied-piper/web", 5, "staging-*"),
	)
	res := &containerCloneResource{client: testClient(srv)}

	tests := []struct {
		name      string
		sourceID  int64
		cloneID   int64
		wantError bool
	}{
		{"a clone of the source", 12, 77, false},
		{"the source itself", 12, 12, true},
		{"another registry", 12, 40, true},
		{"another repository", 12, 41, true},
		{"a missing clone", 12, 99, true},
		{"a missing source", 99, 77, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := res.verifyCloneOfSource(context.Background(), tt.sourceID, tt.cloneID)
			if (err != nil) != tt.wantError {
				t.Errorf("verifyCloneOfSource(%d, %d) = %v, want error %v", tt.sourceID, tt.cloneID, err, tt.wantError)
			}
		})
	}
}

// A tag filter changed in Aikido must show up as drift. The source is carried
// over from state, where Create recorded it.
func TestContainerCloneReadState_RefreshesTheTagAndKeepsTheSource(t *testing.T) {
	state := containerCloneReadState(
		containers.Container{ID: 77, TagFilter: "staging-*"},
		containerCloneModel{
			ID:                types.StringValue("77"),
			SourceContainerID: types.Int64Value(12),
			TagFilter:         types.StringValue("prod-*"),
		},
	)

	if state.ID.ValueString() != "77" {
		t.Errorf("ID = %q, want 77", state.ID.ValueString())
	}
	if state.SourceContainerID.ValueInt64() != 12 {
		t.Errorf("SourceContainerID = %v, want 12", state.SourceContainerID)
	}
	if state.TagFilter.ValueString() != "staging-*" {
		t.Errorf("TagFilter = %q, want the filter Aikido reports", state.TagFilter.ValueString())
	}
}
