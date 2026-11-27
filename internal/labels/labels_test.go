package labels

import (
	"encoding/json"
	"testing"
)

// The API has returned label IDs both as JSON numbers and as strings. Neither
// shape may fail the decode: one bad label would fail the whole page, and with
// it every read that page serves.
func TestLabelID_DecodesNumbersAndStrings(t *testing.T) {
	tests := []struct {
		name string
		body string
		want string
	}{
		{"number", `{"id":123,"name":"payments"}`, "123"},
		{"quoted number", `{"id":"123","name":"payments"}`, "123"},
		{"opaque string", `{"id":"l1","name":"payments"}`, "l1"},
		{"null", `{"id":null,"name":"payments"}`, ""},
		{"absent", `{"name":"payments"}`, ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var label Label
			if err := json.Unmarshal([]byte(tt.body), &label); err != nil {
				t.Fatalf("decoding %s: %v", tt.body, err)
			}

			if label.ID != tt.want {
				t.Errorf("ID = %q, want %q", label.ID, tt.want)
			}
			if label.Name != "payments" {
				t.Errorf("Name = %q, want payments", label.Name)
			}
		})
	}
}

func TestLabel_DecodesRemainingFields(t *testing.T) {
	var label Label

	if err := json.Unmarshal([]byte(`{"id":7,"name":"imported","is_imported":true}`), &label); err != nil {
		t.Fatalf("decoding: %v", err)
	}

	if label.ID != "7" || label.Name != "imported" || !label.IsImported {
		t.Errorf("label = %#v", label)
	}
}
