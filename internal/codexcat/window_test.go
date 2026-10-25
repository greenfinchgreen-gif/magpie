package codexcat

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/yetone/magpie/internal/catalog"
)

// WindowOf reads the model's real context window from the final entry. A
// compaction threshold is a separate field and never changes it.
func TestWindowOfUsesTheWholeWindow(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))

	if window, most, ok := WindowOf([]catalog.Model{
		{ID: "relay/long", Context: 1000000},
		{ID: "relay/compact", Context: 1000000, Compact: 150000},
		{ID: "relay/unknown"},
	}, "relay/long"); !ok || window != 1000000 || most != 0 {
		t.Fatalf("relay/long: window %d, max %d, ok %v; want 1000000, 0, true", window, most, ok)
	}
	if window, most, ok := WindowOf([]catalog.Model{
		{ID: "relay/compact", Context: 1000000, Compact: 150000},
	}, "relay/compact"); !ok || window != 1000000 || most != 0 {
		t.Fatalf("relay/compact: window %d, max %d, ok %v; want 1000000, 0, true", window, most, ok)
	}
	if _, _, ok := WindowOf([]catalog.Model{{ID: "relay/unknown"}}, "relay/unknown"); ok {
		t.Fatal("an unknown window was reported as known")
	}
	if _, _, ok := WindowOf([]catalog.Model{{ID: "relay/long", Context: 1000000}}, "relay/missing"); ok {
		t.Fatal("a missing model was reported as known")
	}
}

// A model Codex lists itself keeps its cached entry, whose window
// WindowOf reports.
func TestWindowOfCodexOwnEntry(t *testing.T) {
	t.Setenv("HOME", t.TempDir())
	t.Setenv("USERPROFILE", os.Getenv("HOME"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(os.Getenv("HOME"), ".config"))
	dir := filepath.Join(os.Getenv("HOME"), ".codex")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	raw, _ := json.Marshal(map[string]any{"models": []any{map[string]any{
		"slug": "gpt-own", "display_name": "GPT Own", "context_window": 1000000,
		"max_context_window": 1000000,
	}}})
	if err := os.WriteFile(filepath.Join(dir, "models_cache.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	window, most, ok := WindowOf([]catalog.Model{{ID: "codex/gpt-own", Context: 1000000}}, "codex/gpt-own")
	if !ok || window != 1000000 || most != 1000000 {
		t.Fatalf("codex/gpt-own: window %d, max %d, ok %v; want 1000000, 1000000, true", window, most, ok)
	}
}
