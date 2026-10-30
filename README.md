# git-tui

A terminal command centre for your repositories. It is **read-only**: it shows local Git state, recent commits, open pull requests, tag-to-tag commit lists, what is deployed to each environment, what is not deployed yet, and deploy runs as they happen, across the repositories you configure.

![git-tui in demo mode: the profile header above the Pull requests view, with a PR's picture in the details pane](docs/images/pull-requests.png)

## Prerequisites

- Go 1.26 or newer. The module declares `go 1.26.0`; with the default `GOTOOLCHAIN=auto`, an older local Go downloads it automatically.
- `git` on `PATH`.
- Optional: the GitHub CLI (`gh`), logged in with `gh auth login`, for the Pull requests, Environments and Deploys views. Each repository's `origin` must point at github.com.
- A terminal at least 60×16. The sidebar appears from 100 columns and the details pane from 120 columns. When the list is narrower than 100 columns, commit, pull request and deploy rows put their title on a second line.

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

`git-tui -version` prints the version it was built from (for example `v0.1.0`, or a pre-release version between releases). The app also shows it in the Connections view and on the `?` help screen.

Without cloning, the same command installs a release: `GOPRIVATE=github.com/acme go install github.com/shaungsimpson/git-tui/cmd/git-tui@latest` (the repository is private, so Go fetches it with your Git credentials).

If it fails:

- `404 Not Found` from `sum.golang.org`: Go asked a public service about a private repository. Set `GOPRIVATE` as above, or once with `go env -w GOPRIVATE=github.com/acme`.
- `could not read Username for 'https://github.com'`: Git cannot log in to GitHub. Run `gh auth setup-git`, or if you use SSH keys, `git config --global url."git@github.com:".insteadOf "https://github.com/"`.
- `command not found: git-tui` after a successful install: add `~/go/bin` to your `PATH`.

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

## Remembered preferences

The time zone (`u`) and the profile header style (`b`) are remembered between runs in `$XDG_STATE_HOME/git-tui/state.json`, which defaults to `~/.local/state/git-tui/state.json`. This is the only file the app writes; your configuration file is never changed. A `-profile` flag wins over the remembered header for that run. Demo mode neither reads nor saves preferences.

## Demo mode

`-demo` uses deterministic built-in fixtures. A `DEMO` badge appears in the header, and the status bar says no repositories are read. The fixtures include one repository that fails to read and one that is missing, so you can see the error and unavailable states. Demo mode shows every feature: the profile header with a generated avatar and every count lit, and PR pictures drawn from generated mock screenshots, all without touching the network. "This week" counts back from the fixtures' own date. Normal mode never falls back to fixtures.

## Views

| View        | Shows                                                                                   |
|-------------|-----------------------------------------------------------------------------------------|
| Overview    | Per repository: branch, worktree state, ahead/behind, latest commit                      |
| Activity    | Recent commits (100 per repository), merged by date, for all repositories or the one selected |
| Pull requests | Open PRs with review state, a ★ when your review is requested (directly or through a team), draft flag and combined check status (passing, pending, failing). Details show the description (first 200 lines, with its pictures) and the changed files; `d` shows the PR's code diff |
| Repository  | Branch, HEAD, upstream, tracking and worktree for the selected repository, plus its commits |
| Tags        | Tags; mark one as the base and pick another to list the **commits** between them        |
| Environments | Per environment, the newest successful GitHub deployment (commit, ref, time) and any newer failed or running attempt. Enter lists commits on `origin/HEAD` that the deployment does not contain |
| Deploys     | Recent deploy workflow runs: status, duration, branch and link. While a run is queued or running, the view reloads every 10 seconds by itself |
| Connections | What is available: local Git, the config, GitHub pull requests, deployments and deploy runs (from what those views last loaded), and git-tui itself: its version, where it is installed and how to update it |

In Pull requests, a **★** in the column after the check status means someone has asked for your review, either you directly or a team you belong to. Details repeats it as "Requested: ★ your review". The star comes from GitHub's `review-requested:@me` search, so it disappears once you submit a review, and comes back if you are asked again. It sits apart from the review column, which shows the PR's overall state (approved, changes, review, draft). If the search fails, the list still loads without stars until the next refresh.

Tag comparisons are plain commit lists. PRs are squash-merged, so each commit title is already its PR title with `(#number)`; no feature summaries are invented. Environments show what **GitHub Deployments recorded**, not what the running service reports: a workflow can record success without the service serving that commit. "Not yet deployed" uses your local clone, so it is as current as your last `git fetch`; if the deployed commit is newer than your clone, the view says so. Ahead/behind reflects your last `git fetch`, since git-tui never fetches.

## Profile header

Above the tabs, a header shows who you are and what is waiting for you, across every configured repository: your GitHub name, handle, company, location and bio, review requests (★), your open PRs, how many of them are failing or have changes requested, your commits this week, repositories with uncommitted changes, followers, and your local git identity. It needs `gh` for the GitHub parts; without it, the header still shows your git identity and the local counts.

Press `b` to cycle it (the choice is remembered), or start with `-profile`:

| Mode    | Shows |
|---------|-------|
| `full`  | Your avatar beside a five-line banner (default) |
| `ascii` | The same banner in plain ASCII, the avatar drawn with characters |
| `text`  | One line of counts, zeros left out |
| `off`   | Nothing |

![The profile header in ASCII mode](docs/images/header-ascii.png)

The header degrades to what the terminal can show:

- Avatar: the real picture, cropped to a circle with an accent-coloured border, in Kitty and Ghostty (outside tmux), coloured half blocks in other terminals with 256 colours or more, characters with fewer colours or `NO_COLOR`, and your initials in a frame until it loads or when there is no picture.
- Height: the banner becomes the one-line header when it would leave the panes fewer than 10 lines, and is hidden when even that would.

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
| `d`               | Show a diff: on a pull request, its code changes; on a repository with uncommitted changes (Overview row, sidebar or Repository view), those changes in sections for staged, unstaged and untracked. `j`/`k` scroll, `PgUp`/`PgDn` page, `Esc` closes |
| `o`               | In Pull requests: open the selected PR in your browser (https links only) |
| `u`               | Show times in UTC or your local time zone (shown in the header)           |
| `b`               | Cycle the profile header: full, ASCII, text, off (always shown at the right of the footer) |
| `Ctrl+C`          | Quit                                                                       |

While the filter input is open, all typing and pasted text go to the input, not to shortcuts.

## Scope and behaviour

- Git is run with argument arrays in each repository's directory, with a 5-second timeout. `GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0` are set, so it never prompts and never refreshes the index.
- Only `status`, `log`, `for-each-ref`, `diff`, `ls-files` and `remote get-url` are run; `diff` uses `--no-ext-diff --no-textconv`, so a repository's own diff programs never run. There is no fetch, pull, checkout or tagging.
- Pull requests are read with `gh pr list`, deployments with `gh api graphql` and deploy runs with `gh run list` (20-second timeout; up to 100 open PRs, 50 environments, the newest 20 deployments per environment and the newest 100 workflow runs).
- A deploy run is a run of a workflow named "deploy" (except pull request checks), or of the same workflow, trigger and branch as a run that recorded a GitHub deployment. That covers pipelines such as a main-branch CI/CD workflow without counting its pull request runs.
- Deploys is the only view that reloads by itself: every 10 seconds while a deploy is queued or running. It stops when nothing is running, when you leave the view, or when a reload fails. Nothing is written to GitHub.
- Pictures in PR descriptions are drawn in the terminal in Ghostty and kitty (Kitty graphics protocol with Unicode placeholders), not inside tmux; other terminals, including WezTerm for now, show the picture's name instead. Only PNG, JPEG and GIF pictures hosted on GitHub are downloaded, up to 10 MB each. Your `gh` login token is sent to github.com only, because attachments in private repositories need it; it is not sent on to GitHub's storage host. Anything else, such as SVG or images on other sites, is named in the text instead. Demo mode never downloads pictures; it draws generated ones.
- A failed load is only retried when you press Ctrl+R; the status line shows a `ctrl+r retry` hint.
- Loads run asynchronously. A refresh keeps the existing content visible, and results from superseded requests are discarded.
- Text from Git (subjects, branch names, errors) has its control characters and escape sequences removed before display.

## Development

```sh
mise run test && mise run build
```

Tests use fixtures and temporary Git repositories. They do not touch remote services.

`mise run screenshots` regenerates the pictures in `docs/images/` from the demo in a real Ghostty window, so the avatar and PR pictures are drawn as they really look. It needs macOS and Ghostty, and the terminal that runs it needs Screen Recording and Accessibility permission (System Settings > Privacy & Security). It presses keys in the window it opens, so leave the keyboard alone for the 20 seconds or so it takes.

Demo mode is the showcase, so it must show every feature. When you add one, give the demo fixtures (`internal/gitx/demo.go`, `demo_images.go`) what it needs and extend `TestDemoShowsEveryFeature`.

CI (`.github/workflows/ci.yml`) runs the same checks plus a build on every pull request to `main` and on every push to `main`.

## Layout

```
cmd/git-tui        entry point and flags
internal/config      JSON config loading and per-repository validation
internal/gitx        read-only git and gh runner, parsers, demo fixtures
internal/ui          Bubble Tea model (model.go), rendering (view.go), key table (keys.go)
```
