package projects

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"worklog/internal/shared/backend"
)

func TestClearReportHistoryPreservesProjectDefinitionsAndUnknownFields(t *testing.T) {
	store := backend.NewStore(t.TempDir())
	raw := []byte(`{"future_config":{"keep":true},"projects":[{"id":"a","name":"Project","issues":["org/repo#1"],"excluded":["org/repo#1"],"future_field":"keep","history":{"2026-11-16":{"review":2}}}]}`)
	path := filepath.Join(store.Dir, "projects.json")
	if err := os.WriteFile(path, raw, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := ClearReportHistory(store); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var before, got map[string]json.RawMessage
	json.Unmarshal(raw, &before)
	json.Unmarshal(after, &got)
	var value map[string]bool
	if err := json.Unmarshal(got["future_config"], &value); err != nil || !value["keep"] {
		t.Fatal("unknown top-level settings lost")
	}
	var entries []map[string]json.RawMessage
	if err := json.Unmarshal(got["projects"], &entries); err != nil {
		t.Fatal(err)
	}
	if _, exists := entries[0]["history"]; exists {
		t.Fatal("report history was not cleared")
	}
	var original []map[string]json.RawMessage
	json.Unmarshal(before["projects"], &original)
	for _, field := range []string{"id", "name", "issues", "excluded", "future_field"} {
		var a, b any
		json.Unmarshal(original[0][field], &a)
		json.Unmarshal(entries[0][field], &b)
		ar, _ := json.Marshal(a)
		br, _ := json.Marshal(b)
		if string(ar) != string(br) {
			t.Fatal("project definition changed", field)
		}
	}
}
