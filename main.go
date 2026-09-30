// herdr-workspace-idx writes each workspace's number into the $idx sidebar
// token so it can be shown with rows = [["$idx", "state_icon", "workspace"], ...].
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"sync"
	"time"
)

type workspace struct {
	ID     string `json:"workspace_id"`
	Number int    `json:"number"`
}

type listResponse struct {
	Result struct {
		Workspaces []workspace `json:"workspaces"`
	} `json:"result"`
}

// runner invokes the herdr CLI and returns its stdout.
type runner func(args ...string) ([]byte, error)

func herdrRunner(bin string) runner {
	return func(args ...string) ([]byte, error) {
		return exec.Command(bin, args...).Output()
	}
}

// maxParallel bounds concurrent herdr CLI calls.
const maxParallel = 8

func syncIdx(run runner) error {
	out, err := run("workspace", "list")
	if err != nil {
		return fmt.Errorf("workspace list: %w", err)
	}
	var resp listResponse
	if err := json.Unmarshal(out, &resp); err != nil {
		return fmt.Errorf("parse workspace list: %w", err)
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)
	for _, ws := range resp.Result.Workspaces {
		wg.Add(1)
		sem <- struct{}{}
		go func(ws workspace) {
			defer wg.Done()
			defer func() { <-sem }()
			// Best effort: a workspace that closed mid-sync is not an error.
			_, _ = run("workspace", "report-metadata", ws.ID,
				"--source", "idx", "--token", "idx="+strconv.Itoa(ws.Number))
		}(ws)
	}
	wg.Wait()
	return nil
}

// settle waits d, then reports whether this invocation is still the newest one.
// Events such as worktree creation fire in bursts; only the last run syncs.
func settle(stateDir string, d time.Duration) bool {
	if d <= 0 {
		return true
	}
	path := filepath.Join(stateDir, "latest")
	mine := strconv.FormatInt(time.Now().UnixNano(), 10) + "-" + strconv.Itoa(os.Getpid())
	if os.WriteFile(path, []byte(mine), 0o644) != nil {
		return true
	}
	time.Sleep(d)
	got, err := os.ReadFile(path)
	return err != nil || string(got) == mine
}

func envOr(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func main() {
	delay := flag.Duration("delay", 0, "wait before syncing, coalescing bursts of events (e.g. 500ms)")
	flag.Parse()

	stateDir := envOr("HERDR_PLUGIN_STATE_DIR", os.TempDir())
	if !settle(stateDir, *delay) {
		return
	}
	if err := syncIdx(herdrRunner(envOr("HERDR_BIN_PATH", "herdr"))); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-workspace-idx:", err)
		os.Exit(1)
	}
}
