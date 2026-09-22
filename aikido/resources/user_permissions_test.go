package resources

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/AikidoTerraform/terraform-provider-aikido/internal/users"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/tfsdk"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// permissionsSchema builds the resource schema the way Terraform would, so the
// tests exercise the same attribute set the provider serves.
func permissionsSchema(t *testing.T) schema.Schema {
	t.Helper()

	var response resource.SchemaResponse
	(&userPermissionsResource{}).Schema(context.Background(), resource.SchemaRequest{}, &response)
	failOn(t, response.Diagnostics)

	return response.Schema
}

func rawConfig(t *testing.T, values map[string]attr.Value) tfsdk.Config {
	t.Helper()

	resourceSchema := permissionsSchema(t)
	attributeValues := make(map[string]attr.Value, len(resourceSchema.Attributes))
	for name, attribute := range resourceSchema.Attributes {
		if supplied, ok := values[name]; ok {
			attributeValues[name] = supplied
			continue
		}
		attributeValues[name] = nullOf(attribute)
	}

	object, diagnostics := types.ObjectValue(objectTypes(resourceSchema), attributeValues)
	failOn(t, diagnostics)

	value, err := object.ToTerraformValue(context.Background())
	if err != nil {
		t.Fatalf("building config value: %v", err)
	}

	return tfsdk.Config{Schema: resourceSchema, Raw: value}
}

// emptyState is a state the resource can write into. A zero tfsdk.State carries
// an untyped raw value, which every SetAttribute then rejects.
func emptyState(t *testing.T, resourceSchema schema.Schema) *tfsdk.State {
	t.Helper()

	return &tfsdk.State{
		Schema: resourceSchema,
		Raw:    tftypes.NewValue(resourceSchema.Type().TerraformType(context.Background()), nil),
	}
}

func objectTypes(resourceSchema schema.Schema) map[string]attr.Type {
	types_ := make(map[string]attr.Type, len(resourceSchema.Attributes))
	for name, attribute := range resourceSchema.Attributes {
		types_[name] = attribute.GetType()
	}

	return types_
}

func nullOf(attribute schema.Attribute) attr.Value {
	switch attribute.GetType() {
	case types.BoolType:
		return types.BoolNull()
	case types.Int64Type:
		return types.Int64Null()
	default:
		return types.StringNull()
	}
}

func failOn(t *testing.T, diagnostics diag.Diagnostics) {
	t.Helper()

	if diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics: %v", diagnostics)
	}
}

func TestConflictDiagnostics(t *testing.T) {
	ctx := context.Background()

	t.Run("a capability contradicting the role is rejected", func(t *testing.T) {
		config := rawConfig(t, map[string]attr.Value{
			"role":             types.StringValue(users.RoleAdmin),
			"can_manage_teams": types.BoolValue(false),
		})

		diagnostics := conflictDiagnostics(ctx, config, "can_manage_teams", true, users.RoleAdmin)
		if !diagnostics.HasError() {
			t.Fatal("want an error for a capability the role overrides")
		}
		detail := diagnostics.Errors()[0].Detail()
		if !strings.Contains(detail, "can_manage_teams") || !strings.Contains(detail, users.RoleAdmin) {
			t.Errorf("detail %q should name the attribute and the role", detail)
		}
	})

	// Redundant, not wrong: the value is what the role would set anyway.
	t.Run("a capability matching the role passes", func(t *testing.T) {
		config := rawConfig(t, map[string]attr.Value{
			"role":             types.StringValue(users.RoleAdmin),
			"can_manage_teams": types.BoolValue(true),
		})

		if diagnostics := conflictDiagnostics(ctx, config, "can_manage_teams", true, users.RoleAdmin); diagnostics.HasError() {
			t.Errorf("got %v, want none", diagnostics)
		}
	})

	t.Run("an omitted capability passes", func(t *testing.T) {
		config := rawConfig(t, map[string]attr.Value{"role": types.StringValue(users.RoleAdmin)})

		if diagnostics := conflictDiagnostics(ctx, config, "can_manage_teams", true, users.RoleAdmin); diagnostics.HasError() {
			t.Errorf("got %v, want none", diagnostics)
		}
	})

	t.Run("read_only true is rejected for an admin", func(t *testing.T) {
		config := rawConfig(t, map[string]attr.Value{
			"role":      types.StringValue(users.RoleAdmin),
			"read_only": types.BoolValue(true),
		})

		if diagnostics := conflictDiagnostics(ctx, config, "read_only", false, users.RoleAdmin); !diagnostics.HasError() {
			t.Error("want an error: admins are never read-only")
		}
	})
}

func TestSetStateFromDetails(t *testing.T) {
	ctx := context.Background()
	resourceSchema := permissionsSchema(t)
	state := emptyState(t, resourceSchema)

	failOn(t, setStateFromDetails(ctx, state, users.Details{
		ID:       42,
		Role:     users.RoleTeamOnly,
		ReadOnly: users.Flag(true),
		Permissions: map[string]users.Flag{
			"can_ignore_issues": users.Flag(true),
		},
	}))

	var id types.String
	var userID types.Int64
	var readOnly, ignoreIssues, manageTeams types.Bool
	failOn(t, state.GetAttribute(ctx, path.Root("id"), &id))
	failOn(t, state.GetAttribute(ctx, path.Root("user_id"), &userID))
	failOn(t, state.GetAttribute(ctx, path.Root("read_only"), &readOnly))
	failOn(t, state.GetAttribute(ctx, path.Root("can_ignore_issues"), &ignoreIssues))
	failOn(t, state.GetAttribute(ctx, path.Root("can_manage_teams"), &manageTeams))

	if id != types.StringValue("42") || userID != types.Int64Value(42) {
		t.Errorf("id = %v, user_id = %v, want both to be 42", id, userID)
	}
	if !readOnly.ValueBool() || !ignoreIssues.ValueBool() {
		t.Errorf("read_only = %v, can_ignore_issues = %v", readOnly, ignoreIssues)
	}
	// Absent from the response, and team_only does not grant it.
	if manageTeams != types.BoolValue(false) {
		t.Errorf("can_manage_teams = %v, want false", manageTeams)
	}
}

// The API reports every capability, so this only matters if that changes: a
// missing key must not contradict what the role guarantees.
func TestSetStateFromDetails_MissingCapabilityFallsBackToTheRole(t *testing.T) {
	ctx := context.Background()
	resourceSchema := permissionsSchema(t)
	state := emptyState(t, resourceSchema)

	failOn(t, setStateFromDetails(ctx, state, users.Details{
		ID:          42,
		Role:        users.RoleAdmin,
		Permissions: map[string]users.Flag{},
	}))

	for _, capability := range users.Capabilities {
		var value types.Bool
		failOn(t, state.GetAttribute(ctx, path.Root(capability), &value))
		if value != types.BoolValue(true) {
			t.Errorf("%s = %v, want true: an admin holds every capability", capability, value)
		}
	}
}

// detailResponse is a detail payload shaped like the API's: every capability is
// present, as Aikido reports them.
func detailResponse(t *testing.T, role string, granted bool) string {
	t.Helper()

	permissions := make(map[string]int, len(users.Capabilities))
	for _, capability := range users.Capabilities {
		if granted {
			permissions[capability] = 1
		} else {
			permissions[capability] = 0
		}
	}
	body, err := json.Marshal(map[string]any{
		"id": 42, "role": role, "read_only": 0, "permissions": permissions,
	})
	if err != nil {
		t.Fatalf("building detail response: %v", err)
	}

	return string(body)
}

// rightsServer answers the rights write and the detail read, recording the body
// that was written so the test can assert on what reached the API.
func rightsServer(t *testing.T, written *map[string]any, details string) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodPut:
			defer r.Body.Close()
			body := map[string]any{}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Errorf("decoding body: %v", err)
			}
			*written = body
			_, _ = io.WriteString(w, `{"success":1}`)
		default:
			_, _ = io.WriteString(w, details)
		}
	}))
	t.Cleanup(srv.Close)

	return srv
}

func TestWrite_SendsEffectivePermissionsAndStoresTheReadBack(t *testing.T) {
	ctx := context.Background()
	resourceSchema := permissionsSchema(t)

	// A demotion from admin to default, with no capability configured. Every one
	// must be written as false, or Aikido keeps the admin's values.
	planValues := map[string]attr.Value{
		"id":        types.StringValue("42"),
		"user_id":   types.Int64Value(42),
		"role":      types.StringValue(users.RoleDefault),
		"read_only": types.BoolValue(false),
	}
	for _, capability := range users.Capabilities {
		planValues[capability] = types.BoolValue(false)
	}
	config := rawConfig(t, planValues)
	plan := tfsdk.Plan{Schema: resourceSchema, Raw: config.Raw}

	var written map[string]any
	srv := rightsServer(t, &written, detailResponse(t, users.RoleDefault, false))

	res := &userPermissionsResource{client: testClient(srv)}
	state := emptyState(t, resourceSchema)
	var diagnostics diag.Diagnostics
	res.write(ctx, plan, state, &diagnostics)
	failOn(t, diagnostics)

	permissions, isMap := written["permissions"].(map[string]any)
	if !isMap {
		t.Fatalf("permissions = %#v, want an object", written["permissions"])
	}
	if len(permissions) != len(users.Capabilities) {
		t.Errorf("sent %d capabilities, want all %d so none is inherited", len(permissions), len(users.Capabilities))
	}
	for capability, value := range permissions {
		if value != false {
			t.Errorf("%s = %v, want false", capability, value)
		}
	}

	var role types.String
	failOn(t, state.GetAttribute(ctx, path.Root("role"), &role))
	if role != types.StringValue(users.RoleDefault) {
		t.Errorf("role = %v, want the value read back", role)
	}
}

func TestWrite_AdminGetsEveryCapabilityRegardlessOfThePlan(t *testing.T) {
	ctx := context.Background()
	resourceSchema := permissionsSchema(t)

	planValues := map[string]attr.Value{
		"id":        types.StringValue("42"),
		"user_id":   types.Int64Value(42),
		"role":      types.StringValue(users.RoleAdmin),
		"read_only": types.BoolValue(false),
	}
	for _, capability := range users.Capabilities {
		planValues[capability] = types.BoolValue(false)
	}
	plan := tfsdk.Plan{Schema: resourceSchema, Raw: rawConfig(t, planValues).Raw}

	var written map[string]any
	srv := rightsServer(t, &written, detailResponse(t, users.RoleAdmin, true))

	res := &userPermissionsResource{client: testClient(srv)}
	var diagnostics diag.Diagnostics
	res.write(ctx, plan, emptyState(t, resourceSchema), &diagnostics)
	failOn(t, diagnostics)

	// users.Effective overrides the plan, so the request matches what Aikido stores.
	permissions := written["permissions"].(map[string]any)
	for capability, value := range permissions {
		if value != true {
			t.Errorf("%s = %v, want true for an admin", capability, value)
		}
	}
}

func TestModifyPlan(t *testing.T) {
	ctx := context.Background()
	resourceSchema := permissionsSchema(t)

	modify := func(t *testing.T, config tfsdk.Config) tfsdk.Plan {
		t.Helper()

		response := resource.ModifyPlanResponse{Plan: tfsdk.Plan{Schema: resourceSchema, Raw: config.Raw}}
		(&userPermissionsResource{}).ModifyPlan(ctx, resource.ModifyPlanRequest{
			Config: config,
			Plan:   tfsdk.Plan{Schema: resourceSchema, Raw: config.Raw},
		}, &response)
		failOn(t, response.Diagnostics)

		return response.Plan
	}

	boolAt := func(t *testing.T, plan tfsdk.Plan, attribute string) types.Bool {
		t.Helper()

		var value types.Bool
		failOn(t, plan.GetAttribute(ctx, path.Root(attribute), &value))

		return value
	}

	// Without this the plan would show an omitted capability keeping its prior
	// value, while the apply writes false.
	t.Run("an omitted capability is planned as false", func(t *testing.T) {
		plan := modify(t, rawConfig(t, map[string]attr.Value{
			"user_id": types.Int64Value(42),
			"role":    types.StringValue(users.RoleDefault),
		}))

		if got := boolAt(t, plan, "can_manage_teams"); got != types.BoolValue(false) {
			t.Errorf("can_manage_teams = %v, want false", got)
		}
		if got := boolAt(t, plan, "read_only"); got != types.BoolValue(false) {
			t.Errorf("read_only = %v, want false", got)
		}
	})

	t.Run("a configured capability survives", func(t *testing.T) {
		plan := modify(t, rawConfig(t, map[string]attr.Value{
			"user_id":         types.Int64Value(42),
			"role":            types.StringValue(users.RoleDefault),
			"can_export_data": types.BoolValue(true),
		}))

		if got := boolAt(t, plan, "can_export_data"); got != types.BoolValue(true) {
			t.Errorf("can_export_data = %v, want true", got)
		}
	})

	// The plan must show what Aikido will store, or the apply reports an
	// inconsistent result.
	t.Run("admin is planned with every capability granted", func(t *testing.T) {
		plan := modify(t, rawConfig(t, map[string]attr.Value{
			"user_id": types.Int64Value(42),
			"role":    types.StringValue(users.RoleAdmin),
		}))

		for _, capability := range users.Capabilities {
			if got := boolAt(t, plan, capability); got != types.BoolValue(true) {
				t.Errorf("%s = %v, want true for an admin", capability, got)
			}
		}
	})

	t.Run("team_only clears what it cannot administer and keeps the rest", func(t *testing.T) {
		plan := modify(t, rawConfig(t, map[string]attr.Value{
			"user_id":           types.Int64Value(42),
			"role":              types.StringValue(users.RoleTeamOnly),
			"can_ignore_issues": types.BoolValue(true),
		}))

		if got := boolAt(t, plan, "can_manage_clouds"); got != types.BoolValue(false) {
			t.Errorf("can_manage_clouds = %v, want false", got)
		}
		if got := boolAt(t, plan, "can_ignore_issues"); got != types.BoolValue(true) {
			t.Errorf("can_ignore_issues = %v, want the configured true", got)
		}
	})
}
