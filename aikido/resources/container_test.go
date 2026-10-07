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
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
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
		{name: "tag_filter", optional: true, computed: true},
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

// Every other optional attribute on this resource treats omission as unmanaged,
// and tag_filter decides which image is scanned. Optional+Computed is what makes
// an omitted attribute arrive unknown rather than null, so a configuration that
// manages only a label cannot reset a filter it never mentioned. Resetting to
// newest-image is then asked for with the empty string, which the validator must
// therefore allow.
func TestContainerSchema_TagFilterIsUnmanagedWhenOmitted(t *testing.T) {
	response := &resource.SchemaResponse{}
	NewContainerResource().Schema(context.Background(), resource.SchemaRequest{}, response)

	attribute, ok := response.Schema.Attributes["tag_filter"]
	if !ok {
		t.Fatal("tag_filter missing from the schema")
	}
	if !attribute.IsOptional() || !attribute.IsComputed() {
		t.Errorf("tag_filter optional=%v computed=%v, want both: an omitted value must arrive unknown",
			attribute.IsOptional(), attribute.IsComputed())
	}

	stringAttribute, ok := attribute.(schema.StringAttribute)
	if !ok {
		t.Fatalf("tag_filter is %T, want schema.StringAttribute", attribute)
	}
	for _, v := range stringAttribute.Validators {
		request := validator.StringRequest{
			Path:        path.Root("tag_filter"),
			ConfigValue: types.StringValue(""),
		}
		validatorResponse := &validator.StringResponse{}
		v.ValidateString(context.Background(), request, validatorResponse)
		if validatorResponse.Diagnostics.HasError() {
			t.Errorf("the empty string is rejected by %T, but it is how a reset is requested", v)
		}
	}
}

// Omitting tag_filter leaves the container's filter alone. Managing a label or a
// code repo link must not change which image Aikido scans.
func TestSetContainerConfig_OmittedTagFilterLeavesTheFilterAlone(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true, TagFilter: "prod-*"}}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:        types.StringValue("1"),
			Active:    types.BoolValue(true),
			TagFilter: types.StringUnknown(),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.tagFilters) != 0 {
		t.Errorf("tagFilters = %v, want no write", api.tagFilters)
	}
	if state.TagFilter.ValueString() != "prod-*" {
		t.Errorf("state.TagFilter = %v, want the filter the container already had", state.TagFilter)
	}
}

// The empty string is how a configuration asks for newest-image scanning, which
// the API expects as null.
func TestSetContainerConfig_EmptyTagFilterResetsToNewestImage(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true, TagFilter: "prod-*"}}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:        types.StringValue("1"),
			Active:    types.BoolValue(true),
			TagFilter: types.StringValue(""),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.tagFilters) != 1 || string(api.tagFilters[0]) != "null" {
		t.Errorf("tagFilters = %v, want [null]", api.tagFilters)
	}
	if state.TagFilter.ValueString() != "" || state.TagFilter.IsNull() {
		t.Errorf("state.TagFilter = %v, want the empty string the configuration asked for", state.TagFilter)
	}
}

// A refresh has no configuration to compare against, so a container scanning its
// newest image has to read back as the empty string. Reading it back as null
// would diff forever against a configuration that spells the reset out.
func TestContainerReadState_NewestImageReadsBackAsTheEmptyString(t *testing.T) {
	state := containerReadState(containers.Container{ID: 1, TagFilter: ""}, nil)

	if state.TagFilter.IsNull() {
		t.Error("tag_filter is null; an explicit tag_filter = \"\" would diff on every plan")
	}
	if state.TagFilter.ValueString() != "" {
		t.Errorf("tag_filter = %q, want the empty string", state.TagFilter.ValueString())
	}
}

// containerAPI records what the write path sent and serves plausible responses.
// current is served from the list endpoint, which is where the write path reads:
// the detail endpoint omits sensitivity and connectivity.
type containerAPI struct {
	activated    []int64
	deactivated  []int64
	tagFilters   []json.RawMessage
	sensitivity  []string
	connectivity []string
	linkedRepos  []int64
	listCalls    int
	current      []containers.Container
}

// containerListPayload re-adds linked_code_repo_id, which Container decodes but
// never marshals, so a fixture can serve it the way the API does.
func containerListPayload(items []containers.Container) []map[string]any {
	payload := make([]map[string]any, 0, len(items))
	for _, item := range items {
		encoded, err := json.Marshal(item)
		if err != nil {
			continue
		}
		var fields map[string]any
		if err := json.Unmarshal(encoded, &fields); err != nil {
			continue
		}
		if item.LinkedCodeRepoID != nil {
			fields["linked_code_repo_id"] = *item.LinkedCodeRepoID
		}
		payload = append(payload, fields)
	}

	return payload
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

		case r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers":
			api.listCalls++
			_ = json.NewEncoder(w).Encode(containerListPayload(api.current))

		default:
			t.Errorf("unexpected %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestSetContainerConfig_WritesEveryManagedAttribute(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{
		ID:       1,
		Name:     "pied-piper/compression",
		Provider: "aws",
		Active:   false,
	}}}
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
			api := &containerAPI{current: []containers.Container{{ID: 1, Active: true, TagFilter: tt.current}}}
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

// A null tag_filter is unmanaged, like an unknown one. Only the empty string
// resets a container to scanning its newest image.
func TestSetContainerConfig_NullTagFilterLeavesTheFilterAlone(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true, TagFilter: "prod-*"}}}
	srv := api.server(t)

	state, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:        types.StringValue("1"),
			Active:    types.BoolValue(true),
			TagFilter: types.StringNull(),
		},
	)
	if err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	if len(api.tagFilters) != 0 {
		t.Errorf("tagFilters = %v, want no write", api.tagFilters)
	}
	if state.TagFilter.ValueString() != "prod-*" {
		t.Errorf("state.TagFilter = %v, want the filter the container already had", state.TagFilter)
	}
}

// sensitivity, connectivity and the code repo link are unmanaged when null, so a
// null config must send nothing at all for them.
func TestSetContainerConfig_SkipsUnmanagedAttributes(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true}}}
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

// A container Aikido reports without a sensitivity or connectivity still has to
// read back what was just written, never unknown.
func TestSetContainerConfig_StateFallsBackToThePlanWhenTheAPIOmitsFields(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true}}}
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
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true}}}
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
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true}}}
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

// Every write is conditional on the container not already holding the planned
// value, so the read has to come first. Activation still precedes the remaining
// writes, so those land on an active container.
func TestSetContainerConfig_ReadsBeforeWriting(t *testing.T) {
	var order []string

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()
		order = append(order, r.Method+" "+r.URL.Path)

		if r.Method == http.MethodGet {
			_ = json.NewEncoder(w).Encode([]containers.Container{{ID: 1, Active: false}})
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
		t.Fatalf("calls = %v, want at least the list read, activate, tag filter", order)
	}
	if order[0] != "GET /public/v1/containers" {
		t.Errorf("first call = %q, want the list read", order[0])
	}
	if !strings.HasSuffix(order[1], "/containers/activate") {
		t.Errorf("second call = %q, want the activate endpoint", order[1])
	}
	if order[2] != "POST /public/v1/containers/updateTagFilter" {
		t.Errorf("third call = %q, want the tag filter write", order[2])
	}
}

// Aikido stamps manually_toggled_active_at before it checks whether the state
// already matches, and that stamp excludes the container from auto-deactivation
// for good. Adopting a container that already holds the planned state must not
// change how Aikido treats it.
func TestSetContainerConfig_SkipsActivationWhenTheStateAlreadyMatches(t *testing.T) {
	tests := []struct {
		name   string
		active bool
	}{
		{"already active", true},
		{"already inactive", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			api := &containerAPI{current: []containers.Container{{ID: 1, Active: tt.active}}}
			srv := api.server(t)

			_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
				context.Background(),
				containerModel{
					ID:     types.StringValue("1"),
					Active: types.BoolValue(tt.active),
				},
			)
			if err != nil {
				t.Fatalf("setContainerConfig: %v", err)
			}

			if len(api.activated) != 0 {
				t.Errorf("activated = %v, want no call", api.activated)
			}
			if len(api.deactivated) != 0 {
				t.Errorf("deactivated = %v, want no call", api.deactivated)
			}
		})
	}
}

// Reposting a value the container already holds makes Aikido redo the work
// behind it — relinking a code repository updates the associated issues and
// AutoFix metadata again — and costs a request against the rate limit.
func TestSetContainerConfig_SkipsWritesThatChangeNothing(t *testing.T) {
	api := &containerAPI{current: []containers.Container{{
		ID:               1,
		Active:           true,
		TagFilter:        "prod-*",
		Sensitivity:      "sensitive",
		Connectivity:     "connected",
		LinkedCodeRepoID: ptrTo(int64(67)),
	}}}
	srv := api.server(t)

	_, err := (&containerResource{client: testClient(srv)}).setContainerConfig(
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

	for name, sent := range map[string]int{
		"activate":     len(api.activated),
		"tagFilter":    len(api.tagFilters),
		"sensitivity":  len(api.sensitivity),
		"connectivity": len(api.connectivity),
		"linkCodeRepo": len(api.linkedRepos),
	} {
		if sent != 0 {
			t.Errorf("%s written %d times, want none", name, sent)
		}
	}
}

// The write path reads the shared list, so adopting many containers costs one
// paginated list rather than a detail GET each.
func TestSetContainerConfig_SharesOneListAcrossContainers(t *testing.T) {
	api := &containerAPI{current: []containers.Container{
		{ID: 1, Active: false},
		{ID: 2, Active: false},
	}}
	srv := api.server(t)
	resource := &containerResource{client: testClient(srv)}

	for _, id := range []string{"1", "2"} {
		if _, err := resource.setContainerConfig(context.Background(), containerModel{
			ID:     types.StringValue(id),
			Active: types.BoolValue(true),
		}); err != nil {
			t.Fatalf("setContainerConfig(%s): %v", id, err)
		}
	}

	if api.listCalls != 1 {
		t.Errorf("list endpoint hit %d times, want 1", api.listCalls)
	}
}

// A data source reading later in the same apply must see what was written, and
// must not pay for a fresh list to get it.
func TestSetContainerConfig_RefreshesTheCachedContainerAfterAWrite(t *testing.T) {
	api := &containerAPI{current: []containers.Container{
		{ID: 1, Active: false, TagFilter: ""},
		{ID: 2, Active: true, Name: "untouched"},
	}}
	srv := api.server(t)
	apiClient := testClient(srv)

	if _, err := (&containerResource{client: apiClient}).setContainerConfig(
		context.Background(),
		containerModel{
			ID:        types.StringValue("1"),
			Active:    types.BoolValue(true),
			TagFilter: types.StringValue("prod-*"),
		},
	); err != nil {
		t.Fatalf("setContainerConfig: %v", err)
	}

	all, err := containers.All(context.Background(), apiClient)
	if err != nil {
		t.Fatalf("All: %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("containers = %d, want 2", len(all))
	}
	if all[0].TagFilter != "prod-*" || !all[0].Active {
		t.Errorf("container 1 = %+v, want the written tag filter and active", all[0])
	}
	if all[1].Name != "untouched" {
		t.Errorf("container 2 = %+v, want it left alone", all[1])
	}
	if api.listCalls != 1 {
		t.Errorf("list endpoint hit %d times, want 1", api.listCalls)
	}
}

// A write that failed may still have changed the container, so the cached list
// cannot be patched from the plan; it has to be dropped.
func TestSetContainerConfig_DropsTheCachedListWhenAWriteFails(t *testing.T) {
	var listCalls int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		if r.Method == http.MethodGet && r.URL.Path == "/public/v1/containers" {
			listCalls++
			_ = json.NewEncoder(w).Encode([]containers.Container{{ID: 1, Active: false}})
			return
		}
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = io.WriteString(w, "boom")
	}))
	t.Cleanup(srv.Close)
	apiClient := testClient(srv)

	if _, err := (&containerResource{client: apiClient}).setContainerConfig(
		context.Background(),
		containerModel{ID: types.StringValue("1"), Active: types.BoolValue(true)},
	); err == nil {
		t.Fatal("setContainerConfig: want an error from the failed activation")
	}

	if _, err := containers.All(context.Background(), apiClient); err != nil {
		t.Fatalf("All: %v", err)
	}
	if listCalls != 2 {
		t.Errorf("list endpoint hit %d times, want 2: the cache must be dropped", listCalls)
	}
}

func ptrTo[T any](value T) *T {
	return &value
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
	api := &containerAPI{current: []containers.Container{{ID: 1, Active: true}}}
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

// An unmanaged tag_filter must not be read as newest-image: that would reset a
// real filter. Only the empty string asks for newest-image.
func TestTagFilterChanged_TreatsUnmanagedAsNoChange(t *testing.T) {
	tests := []struct {
		name    string
		planned types.String
		current string
		want    bool
	}{
		{"unknown over an existing filter", types.StringUnknown(), "prod-*", false},
		{"unknown over an empty filter", types.StringUnknown(), "", false},
		{"null over an existing filter", types.StringNull(), "prod-*", false},
		{"null over an empty filter", types.StringNull(), "", false},
		{"new value", types.StringValue("prod-*"), "", true},
		{"same value", types.StringValue("prod-*"), "prod-*", false},
		{"empty string over an existing filter", types.StringValue(""), "prod-*", true},
		{"empty string over an empty filter", types.StringValue(""), "", false},
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
// nullIfEmptyString guarantees for every string it maps. tag_filter is excluded:
// its empty value is the newest-image state, not an absent one.
func TestContainerModelFromAPI_EmptyStringsBecomeNull(t *testing.T) {
	model := containerModelFromAPI(containers.Container{ID: 1})

	for name, value := range map[string]types.String{
		"sensitivity":         model.Sensitivity,
		"connectivity":        model.Connectivity,
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
