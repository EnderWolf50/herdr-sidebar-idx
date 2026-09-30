package main

import (
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeHerdr answers list commands from canned JSON and records every other call.
type fakeHerdr struct {
	mu    sync.Mutex
	calls []string
	fail  string // fail any command starting with this prefix
}

const (
	workspacesJSON = `{"result":{"workspaces":[{"workspace_id":"w1","number":1},{"workspace_id":"w3","number":2}]}}`
	tabsJSON       = `{"result":{"tabs":[{"tab_id":"w1:t4","number":4},{"tab_id":"w1:t2","number":2},{"tab_id":"w3:t1","number":1}]}}`
	panesJSON      = `{"result":{"panes":[{"pane_id":"w3:p1"},{"pane_id":"w1:p7"},{"pane_id":"w1:p5"},{"pane_id":"w1:p6"}]}}`
	// Listed out of sidebar order on purpose.
	agentsJSON = `{"result":{"agents":[
	  {"pane_id":"w3:p1","tab_id":"w3:t1","workspace_id":"w3"},
	  {"pane_id":"w1:p5","tab_id":"w1:t4","workspace_id":"w1"},
	  {"pane_id":"w1:p7","tab_id":"w1:t2","workspace_id":"w1"},
	  {"pane_id":"w1:p6","tab_id":"w1:t2","workspace_id":"w1"}]}}`
)

func (f *fakeHerdr) run(args ...string) ([]byte, error) {
	cmd := strings.Join(args, " ")
	if f.fail != "" && strings.HasPrefix(cmd, f.fail) {
		return nil, errors.New("boom")
	}
	switch cmd {
	case "workspace list":
		return []byte(workspacesJSON), nil
	case "tab list":
		return []byte(tabsJSON), nil
	case "pane list":
		return []byte(panesJSON), nil
	case "agent list":
		return []byte(agentsJSON), nil
	}
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	f.mu.Unlock()
	return nil, nil
}

func (f *fakeHerdr) sortedCalls() string {
	sort.Strings(f.calls)
	return strings.Join(f.calls, "\n")
}

func TestSyncIdxNumbersWorkspacesAndAgentsInSidebarOrder(t *testing.T) {
	f := &fakeHerdr{}
	if err := syncIdx(f.run, config{WorkspaceNumbers: true, AgentNumbers: true}); err != nil {
		t.Fatal(err)
	}
	// Order: w1 (tab 2: pane list order p7 then p6, then tab 4: p5), then w3.
	want := strings.Join([]string{
		"pane report-metadata w1:p5 --source idx --token idx=3",
		"pane report-metadata w1:p6 --source idx --token idx=2",
		"pane report-metadata w1:p7 --source idx --token idx=1",
		"pane report-metadata w3:p1 --source idx --token idx=4",
		"workspace report-metadata w1 --source idx --token idx=1",
		"workspace report-metadata w3 --source idx --token idx=2",
	}, "\n")
	if got := f.sortedCalls(); got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestSyncIdxClearsTokensWhenDisabled(t *testing.T) {
	f := &fakeHerdr{}
	if err := syncIdx(f.run, config{WorkspaceNumbers: false, AgentNumbers: true}); err != nil {
		t.Fatal(err)
	}
	got := f.sortedCalls()
	for _, want := range []string{
		"workspace report-metadata w1 --source idx --clear-token idx",
		"workspace report-metadata w3 --source idx --clear-token idx",
		"pane report-metadata w1:p7 --source idx --token idx=1",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("missing call %q in\n%s", want, got)
		}
	}
	if strings.Contains(got, "workspace report-metadata w1 --source idx --token") {
		t.Errorf("workspace number set although disabled:\n%s", got)
	}

	f = &fakeHerdr{}
	if err := syncIdx(f.run, config{WorkspaceNumbers: true, AgentNumbers: false}); err != nil {
		t.Fatal(err)
	}
	got = f.sortedCalls()
	if !strings.Contains(got, "pane report-metadata w3:p1 --source idx --clear-token idx") ||
		strings.Contains(got, "pane report-metadata w3:p1 --source idx --token") {
		t.Errorf("agent numbers not cleared:\n%s", got)
	}
}

func TestSyncIdxSurfacesListFailures(t *testing.T) {
	for _, prefix := range []string{"workspace list", "agent list", "tab list", "pane list"} {
		f := &fakeHerdr{fail: prefix}
		if err := syncIdx(f.run, config{true, true}); err == nil {
			t.Errorf("expected an error when %q fails", prefix)
		}
	}
}

func TestSyncIdxRejectsGarbage(t *testing.T) {
	run := func(args ...string) ([]byte, error) { return []byte("not json"), nil }
	if err := syncIdx(run, config{true, true}); err == nil {
		t.Fatal("expected a parse error")
	}
}

func TestParseConfig(t *testing.T) {
	cases := []struct {
		name, in string
		want     config
	}{
		{"empty means both on", "", config{true, true}},
		{"workspace off", "workspace_numbers = false\n", config{false, true}},
		{"agent off", "agent_numbers=false", config{true, false}},
		{"both off with comments", "# c\nworkspace_numbers = false # x\nagent_numbers = false\n", config{false, false}},
		{"crlf line endings", "workspace_numbers = false\r\nagent_numbers = true\r\n", config{false, true}},
		{"bad value ignored", "agent_numbers = maybe\n", config{true, true}},
		{"unknown key ignored", "colour = false\n", config{true, true}},
	}
	for _, c := range cases {
		if got := parseConfig(c.in); got != c.want {
			t.Errorf("%s: got %+v, want %+v", c.name, got, c.want)
		}
	}
}

func TestLoadConfigCreatesDefaultOnFirstRun(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "cfg")
	if got := loadConfig(dir); got != (config{true, true}) {
		t.Fatalf("first run should default to both on, got %+v", got)
	}
	data, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil || !strings.Contains(string(data), "workspace_numbers = true") {
		t.Fatalf("default config not written: %v %q", err, data)
	}
	// A later edit is honoured.
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("agent_numbers = false\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := loadConfig(dir); got != (config{true, false}) {
		t.Fatalf("edit not honoured, got %+v", got)
	}
}

func TestSettleOnlyLastInvocationWins(t *testing.T) {
	dir := t.TempDir()
	results := make([]bool, 3)
	var wg sync.WaitGroup
	for i := range results {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			time.Sleep(time.Duration(i) * 20 * time.Millisecond)
			results[i] = settle(dir, 150*time.Millisecond)
		}(i)
	}
	wg.Wait()
	if results[0] || results[1] || !results[2] {
		t.Fatalf("got %v, want only the last invocation to proceed", results)
	}
}

func TestSettleWithoutDelayRunsImmediately(t *testing.T) {
	if !settle(t.TempDir(), 0) {
		t.Fatal("no delay must always proceed")
	}
}
