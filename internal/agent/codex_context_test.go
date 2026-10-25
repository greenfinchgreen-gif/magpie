package agent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/yetone/magpie/internal/catalog"
	"github.com/yetone/magpie/internal/edit"
	"github.com/yetone/magpie/internal/provider"
)

// contextHome is a Codex home with one provider whose single model has a
// known context window, and no ChatGPT sign-in, so a pick uses
// model_provider = "magpie" and its catalog.
func contextHome(t *testing.T, model string, window int, config string) (home string, path string, read func() string) {
	t.Helper()
	home, read = codexHome(t, "", config)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{model},
	}); err != nil {
		t.Fatal(err)
	}
	cache := catalog.CachePath()
	writeFile(t, cache, `{"relay":{"models":{`+strconv.Quote(model)+`:{`+
		`"id":`+strconv.Quote(model)+`,`+
		`"name":"Relay",`+
		`"limit":{"context":`+strconv.Itoa(window)+`}}}}}`)
	catalog.Reset()
	t.Cleanup(catalog.Reset)
	path = filepath.Join(home, ".codex", "config.toml")
	read = func() string {
		b, _ := os.ReadFile(path)
		return string(b)
	}
	return home, path, read
}

func topInt(t *testing.T, path, key string) (int, bool) {
	t.Helper()
	raw, ok := edit.GetTOMLTop(path, key)
	if !ok {
		return 0, false
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		t.Fatalf("%s = %q: %v", key, raw, err)
	}
	return n, true
}

// Codex's top-level model_context_window mirrors the real context window of
// the model magpie picked. Switching models updates it rather than leaving
// the first model's window to override the second.
func TestCodexContextWindowFollowsPickedModel(t *testing.T) {
	home, path, read := contextHome(t, "long", 1000000, "")
	writeFile(t, catalog.CachePath(), `{"relay":{"models":{`+
		`"long":{"id":"long","name":"Long","limit":{"context":1000000}},`+
		`"small":{"id":"small","name":"Small","limit":{"context":131072}}}}}`)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"long", "small"},
	}); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	cx := codex(home)
	if err := cx.Fields[0].Set("relay/long"); err != nil {
		t.Fatal(err)
	}
	if got, ok := topInt(t, path, codexContextKey); !ok || got != 1000000 {
		t.Fatalf("long: %d, %v\n%s", got, ok, read())
	}
	if got := read(); !strings.Contains(got, "model_context_window = 1000000\n") {
		t.Fatalf("context window was not written as a TOML integer:\n%s", got)
	}
	if err := cx.Fields[0].Set("relay/small"); err != nil {
		t.Fatal(err)
	}
	if got, ok := topInt(t, path, codexContextKey); !ok || got != 131072 {
		t.Fatalf("small: %d, %v\n%s", got, ok, read())
	}
}

// A model whose window isn't known must not keep the previous model's
// top-level window: it would override whatever Codex resolves for it.
func TestCodexContextWindowUnknownGoes(t *testing.T) {
	home, path, read := contextHome(t, "long", 1000000, "")
	writeFile(t, catalog.CachePath(), `{"relay":{"models":{`+
		`"long":{"id":"long","name":"Long","limit":{"context":1000000}},`+
		`"unknown":{"id":"unknown","name":"Unknown"}}}}`)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"long", "unknown"},
	}); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	cx := codex(home)
	if err := cx.Fields[0].Set("relay/long"); err != nil {
		t.Fatal(err)
	}
	if err := cx.Fields[0].Set("relay/unknown"); err != nil {
		t.Fatal(err)
	}
	if _, ok := topInt(t, path, codexContextKey); ok {
		t.Fatalf("stale window left:\n%s", read())
	}
}

// A value the user already wrote is theirs. Magpie neither replaces it on a
// pick nor puts back an older value when it is changed or removed.
func TestCodexContextWindowLeavesTheUsers(t *testing.T) {
	t.Run("already set", func(t *testing.T) {
		home, path, read := contextHome(t, "long", 1000000, "model_context_window = 200000\n")
		if err := codex(home).Fields[0].Set("relay/long"); err != nil {
			t.Fatal(err)
		}
		if got, ok := topInt(t, path, codexContextKey); !ok || got != 200000 {
			t.Fatalf("user value: %d, %v\n%s", got, ok, read())
		}
	})
	t.Run("changed", func(t *testing.T) {
		home, path, read := contextHome(t, "long", 1000000, "")
		cx := codex(home)
		if err := cx.Fields[0].Set("relay/long"); err != nil {
			t.Fatal(err)
		}
		if err := edit.SetTOMLTop(path, edit.KV{Path: codexContextKey, Value: edit.Raw("300000")}); err != nil {
			t.Fatal(err)
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if got, ok := topInt(t, path, codexContextKey); !ok || got != 300000 {
			t.Fatalf("changed value: %d, %v\n%s", got, ok, read())
		}
	})
	t.Run("removed", func(t *testing.T) {
		home, path, read := contextHome(t, "long", 1000000, "")
		cx := codex(home)
		if err := cx.Fields[0].Set("relay/long"); err != nil {
			t.Fatal(err)
		}
		if err := edit.DelTOMLTop(path, codexContextKey); err != nil {
			t.Fatal(err)
		}
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if _, ok := topInt(t, path, codexContextKey); ok {
			t.Fatalf("removed value came back:\n%s", read())
		}
	})
}

// Disconnect removes the mirror magpie wrote. A user's value that replaced
// magpie's, and a user's deletion, both survive Disconnect.
func TestCodexContextWindowDisconnect(t *testing.T) {
	t.Run("magpie's", func(t *testing.T) {
		home, path, read := contextHome(t, "long", 1000000, "")
		cx := codex(home)
		if err := cx.Fields[0].Set("relay/long"); err != nil {
			t.Fatal(err)
		}
		if err := cx.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if _, ok := topInt(t, path, codexContextKey); ok {
			t.Fatalf("magpie's value left:\n%s", read())
		}
	})
	t.Run("user's changed", func(t *testing.T) {
		home, path, read := contextHome(t, "long", 1000000, "")
		cx := codex(home)
		if err := cx.Fields[0].Set("relay/long"); err != nil {
			t.Fatal(err)
		}
		if err := edit.SetTOMLTop(path, edit.KV{Path: codexContextKey, Value: edit.Raw("300000")}); err != nil {
			t.Fatal(err)
		}
		if err := cx.Disconnect(); err != nil {
			t.Fatal(err)
		}
		if got, ok := topInt(t, path, codexContextKey); !ok || got != 300000 {
			t.Fatalf("user's value: %d, %v\n%s", got, ok, read())
		}
	})
}

// Codex writes a routing group's model by its bare name
// (group/auto-gpt-6-1-sol as gpt-6.1-sol). The mirror must use the group's
// window, not a same-named entry Codex cached for another model.
func TestCodexContextWindowResolvesGroupAlias(t *testing.T) {
	home, read := codexHome(t, "", "")
	stop := provider.ServeGroups()
	t.Cleanup(stop)
	for _, id := range []string{"ra", "rb"} {
		if err := provider.Save(provider.Provider{
			ID: id, Name: id, Key: "k", Chat: "https://" + id + ".example/v1",
			Models: []string{"gpt-6.1-sol"},
		}); err != nil {
			t.Fatal(err)
		}
	}
	if got, ok := provider.GroupFor("gpt-6.1-sol"); !ok || got != "group/auto-gpt-6-1-sol" {
		t.Fatalf("GroupFor: %q %v", got, ok)
	}
	writeFile(t, catalog.CachePath(), `{"ra":{"models":{"gpt-6.1-sol":{`+
		`"id":"gpt-6.1-sol","name":"GPT 6.1 Sol","limit":{"context":1000000}}}},`+
		`"rb":{"models":{"gpt-6.1-sol":{`+
		`"id":"gpt-6.1-sol","name":"GPT 6.1 Sol","limit":{"context":1000000}}}}}`)
	catalog.Reset()
	cx := codex(home)
	if err := cx.Apply("model", "group/auto-gpt-6-1-sol"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".codex", "config.toml")
	if err := edit.SetTOMLTop(path, edit.KV{Path: "model", Value: "gpt-6.1-sol"}); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, ok := topInt(t, path, codexContextKey); !ok || got != 1000000 {
		t.Fatalf("group alias: %d, %v\n%s", got, ok, read())
	}
}

// Codex reaches magpie by openai_base_url alone (a ChatGPT sign-in beside
// magpie's models), and its model has a known window: the mirror applies
// there too, not only when magpie is Codex's model_provider.
func TestCodexContextWindowViaBaseURL(t *testing.T) {
	home, path, read := contextHome(t, "long", 1000000, "")
	if err := edit.SetTOMLTop(path,
		edit.KV{Path: "model", Value: "relay/long"},
		edit.KV{Path: "openai_base_url", Value: codexGatewayURL()},
	); err != nil {
		t.Fatal(err)
	}
	if err := codex(home).Sync(); err != nil {
		t.Fatal(err)
	}
	if got, ok := topInt(t, path, codexContextKey); !ok || got != 1000000 {
		t.Fatalf("via base URL: %d, %v\n%s", got, ok, read())
	}
}

// Codex can change its own model outside magpie (its /model, or another
// running Codex app). Sync follows that model's window, rather than leaving
// the model magpie picked earlier to override it.
func TestCodexContextWindowFollowsExternalModel(t *testing.T) {
	home, path, read := contextHome(t, "long", 1000000, "")
	writeFile(t, catalog.CachePath(), `{"relay":{"models":{`+
		`"long":{"id":"long","name":"Long","limit":{"context":1000000}},`+
		`"small":{"id":"small","name":"Small","limit":{"context":131072}}}}}`)
	if err := provider.Save(provider.Provider{
		ID: "relay", Name: "Relay", Key: "k", Chat: "http://127.0.0.1:1/v1",
		Models: []string{"long", "small"},
	}); err != nil {
		t.Fatal(err)
	}
	catalog.Reset()
	cx := codex(home)
	if err := cx.Fields[0].Set("relay/long"); err != nil {
		t.Fatal(err)
	}
	if err := edit.SetTOMLTop(path, edit.KV{Path: "model", Value: "relay/small"}); err != nil {
		t.Fatal(err)
	}
	if err := cx.Sync(); err != nil {
		t.Fatal(err)
	}
	if got, ok := topInt(t, path, codexContextKey); !ok || got != 131072 {
		t.Fatalf("external model: %d, %v\n%s", got, ok, read())
	}
}

// Account failover is a route to magpie too. When it ends, the mirror must
// go with it; a user's own value stays as it is.
func TestCodexContextWindowEndsWithFailover(t *testing.T) {
	claims := func(m map[string]any) string {
		b, _ := json.Marshal(m)
		return "h." + base64.RawURLEncoding.EncodeToString(b) + ".s"
	}
	auth := func(email, acct string) map[string]any {
		return map[string]any{"auth_mode": "chatgpt", "tokens": map[string]any{
			"id_token":      claims(map[string]any{"email": email}),
			"access_token":  claims(map[string]any{"exp": time.Now().Add(time.Hour).Unix()}),
			"refresh_token": "r-" + acct, "account_id": acct}}
	}
	me, _ := json.Marshal(auth("me@example.com", "acct-1"))

	t.Run("magpie's", func(t *testing.T) {
		home, read := codexHome(t, string(me), "model = \"gpt-5.5\"\n")
		b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": true,
			"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
		os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
		cache := filepath.Join(home, ".codex", "models_cache.json")
		writeFile(t, cache, `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1,`+
			`"context_window":1000000,"max_context_window":1000000}]}`)
		cx := codex(home)
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if got, ok := topInt(t, cx.Path, codexContextKey); !ok || got != 1000000 {
			t.Fatalf("failover window: %d, %v\n%s", got, ok, read())
		}
		if err := cx.EndFailover(); err != nil {
			t.Fatal(err)
		}
		if _, ok := topInt(t, cx.Path, codexContextKey); ok {
			t.Fatalf("failover ended, its window still there:\n%s", read())
		}
	})

	t.Run("user's", func(t *testing.T) {
		home, read := codexHome(t, string(me), "model = \"gpt-5.5\"\nmodel_context_window = 300000\n")
		b, _ := json.Marshal([]map[string]any{{"agent": "codex", "user": "spare@example.com", "on": true,
			"seen": time.Now(), "auth": auth("spare@example.com", "acct-2")}})
		os.MkdirAll(filepath.Dir(provider.Path()), 0o755)
		os.WriteFile(filepath.Join(filepath.Dir(provider.Path()), "logins.json"), b, 0o600)
		cache := filepath.Join(home, ".codex", "models_cache.json")
		writeFile(t, cache, `{"models":[{"slug":"gpt-5.5","display_name":"GPT-5.5","priority":1,`+
			`"context_window":1000000,"max_context_window":1000000}]}`)
		cx := codex(home)
		if err := cx.Sync(); err != nil {
			t.Fatal(err)
		}
		if err := cx.EndFailover(); err != nil {
			t.Fatal(err)
		}
		if got, ok := topInt(t, cx.Path, codexContextKey); !ok || got != 300000 {
			t.Fatalf("user's window after failover: %d, %v\n%s", got, ok, read())
		}
	})
}
