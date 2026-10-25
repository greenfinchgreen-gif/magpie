package sessions

import (
	"fmt"
	"path/filepath"
	"testing"
	"time"
)

// A cached file's calls are read into a slice made for them, not grown
// call by call: append's doublings cost a warm start ~500 MB of garbage
// across every cached file, all of it in its first seconds.
func TestLoadedCallsAreSizedOnce(t *testing.T) {
	d := setupCalls(t)
	path := filepath.Join(d.claude, "projects", "p", "s.jsonl")
	st := &callFile{Path: path, Agent: "claude", Began: map[string]time.Time{}}
	add := func(n int) {
		for range n {
			i := len(st.Calls)
			st.Calls = append(st.Calls, Call{Time: time.Unix(int64(i), 0), Agent: "claude", Session: "s", Model: "claude-opus-5-5", Msg: fmt.Sprint("msg", i)})
		}
	}
	add(3000)
	writeCalls(st, 0, false)
	got := loadCalls(file{path: path, agent: "claude"})
	if got == nil || len(got.Calls) != 3000 || got.Calls[2999].Msg != "msg2999" || got.Calls[2999].File != path || got.Msgs["msg2999"] != 2999 {
		t.Fatal("cached calls not read back")
	}
	// made once, to the allocator's size class; grown by append it ends
	// a growth step past
	if cap(got.Calls)-len(got.Calls) > len(got.Calls)/100 {
		t.Fatalf("3000 calls read into a slice of %d", cap(got.Calls))
	}
	// a later frame appended to the file is read after the first, in order
	add(500)
	writeCalls(st, 3000, true)
	got = loadCalls(file{path: path, agent: "claude"})
	if got == nil || len(got.Calls) != 3500 || got.Calls[3499].Msg != "msg3499" || got.Calls[2999].Msg != "msg2999" {
		t.Fatal("appended calls not read back")
	}
}

// Shards are read one after another into one kept frame, and gob leaves a
// field of the zero value out: a call read after one that had a value
// there has none, not the last shard's.
func TestLoadedCallsKeepNothingOfTheLastShard(t *testing.T) {
	d := setupCalls(t)
	write := func(name string, c Call, began time.Time) string {
		path := filepath.Join(d.claude, "projects", "p", name+".jsonl")
		c.Msg = "msg-" + name
		st := &callFile{Path: path, Agent: "claude", Calls: []Call{c}, Began: map[string]time.Time{}}
		if !began.IsZero() {
			st.Began[c.Msg] = began
		}
		writeCalls(st, 0, false)
		return path
	}
	full := write("full", Call{Time: time.Unix(100, 0), Agent: "claude", Session: "s1", Model: "claude-opus-5-5",
		Tokens: Tokens{Input: 50, Output: 7, CacheRead: 9}, Compaction: true}, time.Unix(90, 0))
	bareCall := Call{Time: time.Unix(200, 0), Agent: "claude"}
	empty := write("empty", bareCall, time.Time{})
	for range 3 {
		if got := loadCalls(file{path: full, agent: "claude"}); got == nil || got.Calls[0].Input != 50 || !got.Calls[0].Compaction {
			t.Fatal("the full shard not read back")
		}
		got := loadCalls(file{path: empty, agent: "claude"})
		if got == nil || len(got.Calls) != 1 {
			t.Fatal("the bare shard not read back")
		}
		want := bareCall
		want.Msg, want.File = "msg-empty", empty
		if c := got.Calls[0]; c.Input != 0 || c.Output != 0 || c.CacheRead != 0 || c.Compaction || c.Session != "" || c.Model != "" || c.Msg != want.Msg || !c.Time.Equal(want.Time) {
			t.Fatalf("read after the full shard: %+v", c)
		}
		if b, ok := got.Began["msg-empty"]; ok {
			t.Fatalf("began %v kept from the last shard", b)
		}
	}
}
