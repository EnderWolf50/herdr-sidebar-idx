// herdr-sidebar-idx writes numbers into the $idx sidebar token of workspaces
// and agent panes, so they can be shown with rows such as
// [["$idx", "state_icon", "workspace"], ...] and matched to jump shortcuts.
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type workspace struct {
	ID     string `json:"workspace_id"`
	Number int    `json:"number"`
}

type tab struct {
	ID     string `json:"tab_id"`
	Number int    `json:"number"`
}

type pane struct {
	ID string `json:"pane_id"`
}

type agent struct {
	PaneID      string `json:"pane_id"`
	TabID       string `json:"tab_id"`
	WorkspaceID string `json:"workspace_id"`
}

// config is read from config.toml in the plugin config directory.
type config struct {
	WorkspaceNumbers bool
	AgentNumbers     bool
}

const defaultConfig = `# herdr-sidebar-idx settings.
# Run "herdr plugin action invoke enderwolf50.sidebar-idx.sync" to apply changes.

# Number workspaces in the sidebar ($idx in [ui.sidebar.spaces] rows).
workspace_numbers = true

# Number agents in the sidebar ($idx in [ui.sidebar.agents] rows).
agent_numbers = true
`

// parseConfig reads "key = true|false" lines; unknown keys and bad values are ignored.
func parseConfig(data string) config {
	c := config{WorkspaceNumbers: true, AgentNumbers: true}
	for _, line := range strings.Split(data, "\n") {
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = line[:i]
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		b, err := strconv.ParseBool(strings.TrimSpace(v))
		if err != nil {
			continue
		}
		switch strings.TrimSpace(k) {
		case "workspace_numbers":
			c.WorkspaceNumbers = b
		case "agent_numbers":
			c.AgentNumbers = b
		}
	}
	return c
}

// loadConfig reads the config, creating a commented default on first run.
func loadConfig(dir string) config {
	path := filepath.Join(dir, "config.toml")
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			_ = os.MkdirAll(dir, 0o755)
			_ = os.WriteFile(path, []byte(defaultConfig), 0o644)
		}
		return parseConfig("")
	}
	return parseConfig(string(data))
}

// runner invokes the herdr CLI and returns its stdout.
type runner func(args ...string) ([]byte, error)

func herdrRunner(bin string) runner {
	return func(args ...string) ([]byte, error) {
		return exec.Command(bin, args...).Output()
	}
}

// list runs a herdr list command and decodes result.<key> into out.
func list(run runner, key string, out any, args ...string) error {
	raw, err := run(args...)
	if err != nil {
		return fmt.Errorf("%s: %w", strings.Join(args, " "), err)
	}
	var resp struct {
		Result map[string]json.RawMessage `json:"result"`
	}
	if err := json.Unmarshal(raw, &resp); err != nil {
		return fmt.Errorf("parse %s: %w", strings.Join(args, " "), err)
	}
	if err := json.Unmarshal(resp.Result[key], out); err != nil {
		return fmt.Errorf("parse %s: %w", key, err)
	}
	return nil
}

// job sets or clears the idx token on one workspace or pane.
type job struct {
	kind   string // "workspace" or "pane"
	id     string
	number int
	on     bool
}

func (j job) args() []string {
	a := []string{j.kind, "report-metadata", j.id, "--source", "idx"}
	if j.on {
		return append(a, "--token", "idx="+strconv.Itoa(j.number))
	}
	return append(a, "--clear-token", "idx")
}

// agentOrder numbers agents 1..N in sidebar order: workspace, then tab, then
// pane order. This matches the default "spaces" agent panel sort.
func agentOrder(agents []agent, wsNum, tabNum map[string]int, paneIdx map[string]int) []agent {
	sorted := append([]agent(nil), agents...)
	sort.SliceStable(sorted, func(i, j int) bool {
		a, b := sorted[i], sorted[j]
		if wsNum[a.WorkspaceID] != wsNum[b.WorkspaceID] {
			return wsNum[a.WorkspaceID] < wsNum[b.WorkspaceID]
		}
		if tabNum[a.TabID] != tabNum[b.TabID] {
			return tabNum[a.TabID] < tabNum[b.TabID]
		}
		return paneIdx[a.PaneID] < paneIdx[b.PaneID]
	})
	return sorted
}

// maxParallel bounds concurrent herdr CLI calls.
const maxParallel = 8

func syncIdx(run runner, cfg config) error {
	var workspaces []workspace
	if err := list(run, "workspaces", &workspaces, "workspace", "list"); err != nil {
		return err
	}
	jobs := make([]job, 0, len(workspaces))
	for _, ws := range workspaces {
		jobs = append(jobs, job{"workspace", ws.ID, ws.Number, cfg.WorkspaceNumbers})
	}

	var agents []agent
	if err := list(run, "agents", &agents, "agent", "list"); err != nil {
		return err
	}
	var tabs []tab
	var panes []pane
	if err := list(run, "tabs", &tabs, "tab", "list"); err != nil {
		return err
	}
	if err := list(run, "panes", &panes, "pane", "list"); err != nil {
		return err
	}
	wsNum := map[string]int{}
	for _, ws := range workspaces {
		wsNum[ws.ID] = ws.Number
	}
	tabNum := map[string]int{}
	for _, t := range tabs {
		tabNum[t.ID] = t.Number
	}
	paneIdx := map[string]int{}
	for i, p := range panes {
		paneIdx[p.ID] = i
	}
	for i, a := range agentOrder(agents, wsNum, tabNum, paneIdx) {
		jobs = append(jobs, job{"pane", a.PaneID, i + 1, cfg.AgentNumbers})
	}

	var wg sync.WaitGroup
	sem := make(chan struct{}, maxParallel)
	for _, j := range jobs {
		wg.Add(1)
		sem <- struct{}{}
		go func(j job) {
			defer wg.Done()
			defer func() { <-sem }()
			// Best effort: a workspace or pane that closed mid-sync is not an error.
			_, _ = run(j.args()...)
		}(j)
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
	cfg := loadConfig(envOr("HERDR_PLUGIN_CONFIG_DIR", stateDir))
	if err := syncIdx(herdrRunner(envOr("HERDR_BIN_PATH", "herdr")), cfg); err != nil {
		fmt.Fprintln(os.Stderr, "herdr-sidebar-idx:", err)
		os.Exit(1)
	}
}
