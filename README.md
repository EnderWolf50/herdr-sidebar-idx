# herdr-sidebar-idx

A [herdr](https://herdr.dev) plugin that writes numbers into the `$idx` sidebar
token of workspaces and agents, so the sidebar can show the number you press to
jump to them.

herdr has no built-in token for these numbers. This plugin reads them from the
herdr CLI and reports them with `report-metadata`.

## Install

Requires Go (the plugin is built on install).

```
herdr plugin install enderwolf50/herdr-sidebar-idx
```

For local development, build and link the checkout:

```
go build -o sidebar-idx .      # sidebar-idx.exe on Windows
herdr plugin link /path/to/herdr-sidebar-idx
```

## Show the numbers

Add `$idx` to the rows in herdr's `config.toml`, then `herdr server reload-config`:

```toml
[ui.sidebar.spaces]
rows = [
  ["$idx", "state_icon", "workspace"],
  ["branch", "git_status"],
]

[ui.sidebar.agents]
rows = [
  ["$idx", "state_icon", "machine", "workspace", "tab"],
  ["agent"],
]
```

## Settings

`herdr plugin config-dir enderwolf50.sidebar-idx` prints the plugin's config
directory. It holds `config.toml`, created with defaults on first run:

```toml
workspace_numbers = true
agent_numbers = true
```

Set either to `false` to hide those numbers; the plugin clears the token, so
`$idx` disappears from the rows. Apply a change with:

```
herdr plugin action invoke enderwolf50.sidebar-idx.sync
```

Only `key = true|false` lines are read; `#` starts a comment.

## Numbering

- **Workspaces**: herdr's own workspace number, the one `switch_workspace` uses.
- **Agents**: 1..N in workspace, then tab, then pane order. This matches
  the default `ui.agent_panel_sort = "spaces"`. With `"priority"`, herdr orders
  agents by attention and the numbers will not match the sidebar.

## When it runs

- **startup**: once after herdr restores its session. Metadata does not survive
  a server restart, so this puts the numbers back.
- **events**: `workspace.created/closed/moved/reordered`, `tab.created/closed`,
  `pane.created/closed/moved`, `pane.agent_detected`. Runs wait 500ms and only
  the newest run in a burst syncs.
- **action**: `herdr plugin action invoke enderwolf50.sidebar-idx.sync`.

## Develop

```
go vet ./... && go test -race ./...
```
