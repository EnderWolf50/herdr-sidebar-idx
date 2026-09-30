package main

import (
	"errors"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
)

const listJSON = `{"id":"cli:workspace:list","result":{"type":"workspace_list","workspaces":[
  {"workspace_id":"w1","number":1,"label":"a"},
  {"workspace_id":"w3","number":2,"label":"b"},
  {"workspace_id":"w5","number":3,"label":"c"}]}}`

func TestSyncIdxReportsEveryWorkspaceNumber(t *testing.T) {
	var mu sync.Mutex
	var reports []string
	run := func(args ...string) ([]byte, error) {
		if args[0] == "workspace" && args[1] == "list" {
			return []byte(listJSON), nil
		}
		mu.Lock()
		reports = append(reports, strings.Join(args, " "))
		mu.Unlock()
		return nil, nil
	}

	if err := syncIdx(run); err != nil {
		t.Fatal(err)
	}
	sort.Strings(reports)
	want := []string{
		"workspace report-metadata w1 --source idx --token idx=1",
		"workspace report-metadata w3 --source idx --token idx=2",
		"workspace report-metadata w5 --source idx --token idx=3",
	}
	if strings.Join(reports, "\n") != strings.Join(want, "\n") {
		t.Fatalf("got\n%s\nwant\n%s", strings.Join(reports, "\n"), strings.Join(want, "\n"))
	}
}

func TestSyncIdxSurfacesListFailure(t *testing.T) {
	run := func(args ...string) ([]byte, error) { return nil, errors.New("no server") }
	if err := syncIdx(run); err == nil {
		t.Fatal("expected an error when herdr is unreachable")
	}
}

func TestSyncIdxRejectsGarbage(t *testing.T) {
	run := func(args ...string) ([]byte, error) { return []byte("not json"), nil }
	if err := syncIdx(run); err == nil {
		t.Fatal("expected a parse error")
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
