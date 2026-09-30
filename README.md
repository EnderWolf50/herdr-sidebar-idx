# herdr-workspace-idx

A [herdr](https://herdr.dev) plugin that writes each workspace's number into
the `$idx` sidebar token, so the sidebar can show the number you press to jump
to it.

herdr has no built-in token for a workspace's number. This plugin reads it from
`herdr workspace list` and reports it with `herdr workspace report-metadata`.

## Install

Requires Go (the plugin is built on install).

```
herdr plugin install enderwolf50/herdr-workspace-idx
```

For a private repo, or local development, clone it and link the checkout:

```
go build -o workspace-idx .      # workspace-idx.exe on Windows
herdr plugin link /path/to/herdr-workspace-idx
```

## Show the number

Add `$idx` to the space rows in `config.toml`, then `herdr server reload-config`:

```toml
[ui.sidebar.spaces]
rows = [
  ["$idx", "state_icon", "workspace"],
  ["branch", "git_status"],
]
```

## When it runs

- **startup**: once after herdr restores its session. Metadata does not survive
  a server restart, so this puts the numbers back.
- **events**: `workspace.created`, `closed`, `moved`, `reordered`. Runs wait
  500ms and only the newest run in a burst syncs.
- **action**: `herdr plugin action invoke enderwolf50.workspace-idx.sync`.

## Develop

```
go vet ./... && go test -race ./...
```
