# git-tui

A terminal command centre for your repositories. It is **read-only**: it shows local Git state, recent commits, open pull requests, tag-to-tag commit lists, what is deployed to each environment, what is not deployed yet, and deploy runs as they happen, across the repositories you configure.

## Prerequisites

- Go 1.26 or newer. The module declares `go 1.26.0`; with the default `GOTOOLCHAIN=auto`, an older local Go downloads it automatically.
- `git` on `PATH`.
- Optional: the GitHub CLI (`gh`), logged in with `gh auth login`, for the Pull requests, Environments and Deploys views. Each repository's `origin` must point at github.com.
- A terminal at least 60×16. The sidebar appears from 100 columns and the details pane from 120 columns.

No Nerd Font or image support is required.

## Build and run

Tasks run with [mise](https://mise.jdx.dev). On first use in this folder, run `mise trust`.

```sh
mise tasks                  # list tasks
mise run build              # builds bin/git-tui
mise run test               # gofmt check, go vet, tests with the race detector
mise run demo               # builds and runs with fixture data, no repositories read
mise run start              # builds and runs with the default config location
mise run start -- -config examples/config.example.json
mise run install-cli        # installs the git-tui command into ~/go/bin
```

After `mise run install-cli`, run `git-tui` from anywhere (`~/go/bin` must be on your `PATH`). Run it again after pulling changes.

## Configuration

The configuration file is a small JSON file:

```json
{
  "repositories": [
    { "name": "backend", "path": "~/Code/backend" },
    { "name": "frontend", "path": "~/Code/frontend" }
  ]
}
```

The file is selected in this order:

1. `-config FILE`
2. `$GIT_TUI_CONFIG`
3. `$XDG_CONFIG_HOME/git-tui/config.json`, which defaults to `~/.config/git-tui/config.json`

`~` is expanded. Each repository is validated on its own: a missing directory or a directory without `.git` is marked unavailable, and the app still opens. If no configuration file exists, the app opens and explains how to create one. Start from `examples/config.example.json`.

## Demo mode

`-demo` uses deterministic built-in fixtures. A `DEMO` badge appears in the header, and the status bar says no repositories are read. The fixtures include one repository that fails to read and one that is missing, so you can see the error and unavailable states. Normal mode never falls back to fixtures.

## Views

| View        | Shows                                                                                   |
|-------------|-----------------------------------------------------------------------------------------|
| Overview    | Per repository: branch, worktree state, ahead/behind, latest commit                      |
| Activity    | Recent commits (100 per repository), merged by date, for all repositories or the one selected |
| Pull requests | Open PRs with review state, draft flag and combined check status (passing, pending, failing) |
| Repository  | Branch, HEAD, upstream, tracking and worktree for the selected repository, plus its commits |
| Tags        | Tags; mark one as the base and pick another to list the **commits** between them        |
| Environments | Per environment, the newest successful GitHub deployment (commit, ref, time) and any newer failed or running attempt. Enter lists commits on `origin/HEAD` that the deployment does not contain |
| Deploys     | Recent deploy workflow runs: status, duration, branch and link. While a run is queued or running, the view reloads every 10 seconds by itself |
| Connections | What is available: local Git, the config, GitHub pull requests, deployments and deploy runs (from what those views last loaded) |

Tag comparisons are plain commit lists. PRs are squash-merged, so each commit title is already its PR title with `(#number)`; no feature summaries are invented. Environments show what **GitHub Deployments recorded**, not what the running service reports: a workflow can record success without the service serving that commit. "Not yet deployed" uses your local clone, so it is as current as your last `git fetch`; if the deployed commit is newer than your clone, the view says so. Ahead/behind reflects your last `git fetch`, since git-tui never fetches.

## Keyboard

| Key               | Action                                                                     |
|-------------------|----------------------------------------------------------------------------|
| `j` `k` / `↓` `↑` | Move the selection in the focused pane                                     |
| `Tab` / `Shift+Tab` | Move focus between visible panes                                         |
| `Enter`           | Open or inspect the selected item (in Tags: choose the base tag, then the head tag; in Environments: list commits not yet deployed) |
| `Esc`             | Close details, the comparison, the base selection or the filter            |
| `/`               | Filter the current list (`Enter` applies, `Esc` cancels)                   |
| `g` then `o` `a` `p` `r` `t` `e` `d` `c` | Go to Overview, Activity, Pull requests, Repository, Tags, Environments, Deploys or Connections (1 second timeout) |
| `Ctrl+R`          | Refresh the current view                                                   |
| `?`               | Contextual shortcut help                                                   |
| `F2`              | Toggle the dark and light themes                                           |
| `o`               | In Pull requests: open the selected PR in your browser (https links only) |
| `u`               | Show times in UTC or your local time zone (shown in the header)           |
| `Ctrl+C`          | Quit                                                                       |

While the filter input is open, all typing and pasted text go to the input, not to shortcuts.

## Scope and behaviour

- Git is run with argument arrays in each repository's directory, with a 5-second timeout. `GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0` are set, so it never prompts and never refreshes the index.
- Only `status`, `log`, `for-each-ref` and `remote get-url` are run. There is no fetch, pull, checkout or tagging.
- Pull requests are read with `gh pr list`, deployments with `gh api graphql` and deploy runs with `gh run list` (20-second timeout; up to 100 open PRs, 50 environments, the newest 20 deployments per environment and the newest 100 workflow runs).
- A deploy run is a run of a workflow named "deploy" (except pull request checks), or of the same workflow, trigger and branch as a run that recorded a GitHub deployment. That covers pipelines such as a main-branch CI/CD workflow without counting its pull request runs.
- Deploys is the only view that reloads by itself: every 10 seconds while a deploy is queued or running. It stops when nothing is running, when you leave the view, or when a reload fails. Nothing is written to GitHub.
- A failed load is only retried when you press Ctrl+R; the status line shows a `ctrl+r retry` hint.
- Loads run asynchronously. A refresh keeps the existing content visible, and results from superseded requests are discarded.
- Text from Git (subjects, branch names, errors) has its control characters and escape sequences removed before display.

## Development

```sh
mise run test && mise run build
```

Tests use fixtures and temporary Git repositories. They do not touch remote services.

## Layout

```
cmd/git-tui        entry point and flags
internal/config      JSON config loading and per-repository validation
internal/gitx        read-only git and gh runner, parsers, demo fixtures
internal/ui          Bubble Tea model (model.go), rendering (view.go), key table (keys.go)
```
