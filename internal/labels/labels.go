// Package labels models an Aikido label.
package labels

import (
	"encoding/json"
	"fmt"
)

type Label struct {
	ID         string `json:"id"`
	Name       string `json:"name"`
	IsImported bool   `json:"is_imported"`
}

// UnmarshalJSON accepts a label ID as either a JSON number or a string. The API
// documents an integer, but a whole page decodes in one call, so any shape this
// type rejects fails every read rather than a single label.
func (l *Label) UnmarshalJSON(data []byte) error {
	var raw struct {
		ID         json.RawMessage `json:"id"`
		Name       string          `json:"name"`
		IsImported bool            `json:"is_imported"`
	}
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	l.Name = raw.Name
	l.IsImported = raw.IsImported
	l.ID = ""

	switch {
	case len(raw.ID) == 0 || string(raw.ID) == "null":
	case raw.ID[0] == '"':
		if err := json.Unmarshal(raw.ID, &l.ID); err != nil {
			return fmt.Errorf("label id: %w", err)
		}
	default:
		l.ID = string(raw.ID)
	}

	return nil
}
