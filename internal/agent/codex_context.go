package agent

import (
	"encoding/json"
	"fmt"
	"maps"
	"strconv"
	"strings"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/codexcat"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// Codex's top-level model_context_window overrides the context_window of
// the selected model's catalog entry (codex-rs ModelInfo::override_context_
// window: context_window.min(max_context_window), or context_window when
// there is no max). Codex++ writes one fixed window there, which makes every
// model in a group run to that size. magpie instead keeps the entry's own
// per-model window as the source of truth and mirrors only the selected
// model into this top-level key for compatibility with Codex versions and
// configurations that read it before the catalog. This is the model's real
// context window, never its compaction threshold.
//
// The key belongs to the user unless the stash says magpie wrote it. A value
// changed after magpie wrote it becomes the user's: it is never overwritten
// again, and Disconnect leaves it as it is.
const codexContextKey = "model_context_window"

// codexContextOwned is the top-level window magpie wrote. A value the user
// had before that is never replaced, so the stash only needs magpie's own.
type codexContextOwned struct {
	Value string `json:"value"`
}

// syncTopWindow mirrors the real context window of the selected model into
// Codex's top-level model_context_window, or removes magpie's value when the
// model has no known window. ms is the model list for the configured route.
// It never overwrites a key the user owns.
func syncTopWindow(path, key string, ms []catalog.Model, model string) error {
	cur, hasCur := edit.GetTOMLTop(path, codexContextKey)
	owned := contextStash(key)
	if owned.Value != "" && !hasCur {
		// The user removed magpie's key since the last write.
		forget(key)
		return nil
	}
	if hasCur && (owned.Value == "" || cur != owned.Value) {
		// The user set the key, or changed magpie's value since: it is
		// theirs from now on.
		forget(key)
		return nil
	}

	window, ok := codexWindowOf(ms, model)
	want := ""
	if ok && window > 0 {
		want = strconv.Itoa(window)
	}
	if want == "" {
		if owned.Value == "" {
			return nil
		}
		// The selected model's window is unknown: a stale value would
		// override whatever entry Codex finds next.
		if err := edit.DelTOMLTop(path, codexContextKey); err != nil {
			return err
		}
		forget(key)
		return nil
	}
	if owned.Value != "" && cur == want {
		return nil
	}
	if err := edit.SetTOMLTop(path, edit.KV{Path: codexContextKey, Value: edit.Raw(want)}); err != nil {
		return err
	}
	return saveContextStash(key, codexContextOwned{Value: want})
}

// codexWindowOf resolves the window Codex will use for model. A bare model
// with no matching catalog id may be how Codex writes a routing group alias
// (group/auto-gpt-6-1-sol is written gpt-6.1-sol, #750), so that group is
// resolved before Codex's own cache. A codex/ id is Codex's own model and
// keeps its catalog entry first.
func codexWindowOf(ms []catalog.Model, model string) (int, bool) {
	if slug, own := strings.CutPrefix(model, "codex/"); own {
		window, _, ok := codexcat.WindowOf(ms, model)
		if ok {
			return window, true
		}
		return nativeWindow(slug)
	}
	if window, _, ok := codexcat.WindowOf(ms, model); ok {
		return window, true
	}
	if model != "" && !strings.Contains(model, "/") {
		// GroupFor resolves both a found group's model spelling and a
		// group's name. The catalog may list a found group by its public
		// auto/<model> id rather than its canonical group/auto-<slug>.
		if gid, ok := provider.GroupFor(model); ok {
			for _, id := range []string{gid, provider.PublicRef(gid)} {
				if window, _, ok := codexcat.WindowOf(ms, id); ok {
					return window, true
				}
			}
		}
		if window, ok := nativeWindow(model); ok {
			return window, true
		}
	}
	return 0, false
}

// nativeWindow is the final window of one of Codex's own models, by the bare
// slug Codex names it by: the entry Codex cached, with a window the user set
// on the codex provider applied as the gateway applies it.
func nativeWindow(slug string) (int, bool) {
	raw, found := codexcat.CacheEntries()[slug]
	if !found {
		return 0, false
	}
	entry := maps.Clone(raw)
	if n, max, ok := provider.CodexNativeWindow(true)(slug); ok {
		codexcat.Window(entry, n, max)
	}
	window, _, ok := codexcat.WindowOfEntry(entry)
	return window, ok
}

// restoreTopWindow removes the window magpie wrote, while it is still the
// one there. A value the user changed or removed since is theirs and stays.
func restoreTopWindow(path, key string) error {
	owned := contextStash(key)
	if owned.Value == "" {
		return nil
	}
	cur, has := edit.GetTOMLTop(path, codexContextKey)
	if !has {
		// Removed after magpie wrote it: the user's deletion is final,
		// so don't put an older value back.
		forget(key)
		return nil
	}
	if cur != owned.Value {
		// changed by the user since: theirs
		forget(key)
		return nil
	}
	if err := edit.DelTOMLTop(path, codexContextKey); err != nil {
		return err
	}
	forget(key)
	return nil
}

func contextStash(key string) codexContextOwned {
	raw := stashLoad()[key]
	if raw == "" {
		return codexContextOwned{}
	}
	var o codexContextOwned
	if json.Unmarshal([]byte(raw), &o) != nil || o.Value == "" {
		return codexContextOwned{}
	}
	return o
}

func saveContextStash(key string, o codexContextOwned) error {
	b, err := json.Marshal(o)
	if err != nil {
		return fmt.Errorf("codex context window: %w", err)
	}
	stash(map[string]string{key: string(b)})
	return nil
}
