package logx

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func decodeLines(t *testing.T, output string) []map[string]any {
	t.Helper()
	var events []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(output), "\n") {
		var event map[string]any
		if err := json.Unmarshal([]byte(line), &event); err != nil {
			t.Fatalf("line %q is not JSON: %v", line, err)
		}
		events = append(events, event)
	}
	return events
}

func TestJSONModeWritesOneEventPerLineToOut(t *testing.T) {
	var out, errOut bytes.Buffer
	log := New(&out, &errOut, false, true)
	log.Upload("src/app.go")
	log.Change("add", "src/new.go")
	log.Error("boom")
	log.SyncAllDone(0, 3)

	if errOut.Len() != 0 {
		t.Fatalf("stderr=%q; want every JSON event on stdout", errOut.String())
	}
	events := decodeLines(t, out.String())
	if len(events) != 4 {
		t.Fatalf("got %d events; want 4", len(events))
	}
	checks := []map[string]any{
		{"event": "upload", "level": "info", "path": "src/app.go"},
		{"event": "change", "change": "add", "path": "src/new.go"},
		{"event": "error", "level": "error", "message": "boom"},
		{"event": "syncAllDone", "uploaded": float64(0), "total": float64(3)},
	}
	for i, want := range checks {
		for key, value := range want {
			if events[i][key] != value {
				t.Errorf("event %d: %s=%v; want %v", i, key, events[i][key], value)
			}
		}
		if _, ok := events[i]["time"]; !ok {
			t.Errorf("event %d has no time", i)
		}
	}
}

func TestJSONModeRespectsQuiet(t *testing.T) {
	var out bytes.Buffer
	log := New(&out, &out, true, true)
	log.Upload("a")
	log.Change("change", "a")
	log.Info("kept")
	events := decodeLines(t, out.String())
	if len(events) != 1 || events[0]["event"] != "info" {
		t.Fatalf("events=%v; want only the info event", events)
	}
}

func TestTextModeHasNoJSON(t *testing.T) {
	var out bytes.Buffer
	New(&out, &out, false, false).Upload("a.go")
	if strings.HasPrefix(out.String(), "{") || !strings.Contains(out.String(), "[upload]") {
		t.Fatalf("output=%q; want terminal line", out.String())
	}
}
