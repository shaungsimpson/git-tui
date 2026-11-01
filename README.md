# git-tui

A terminal command centre for your repositories. It is **read-only**: it shows local Git state, recent commits, open pull requests, tag-to-tag commit lists, what is deployed to each environment, what is not deployed yet, and deploy runs as they happen, across the repositories you configure.

![git-tui in demo mode: the profile header, with its review and ready-to-merge badges, above the Pull requests view, with a PR's picture in the details pane](docs/images/pull-requests.png)

## Get started

You need Git, a GitHub account with access to `acme`, and Go 1.21 or newer, which fetches the Go version the app needs by itself (see [Prerequisites](#prerequisites)).

1. Install the `git-tui` command. The repository is private, so `GOPRIVATE` tells Go to fetch it with your Git credentials instead of asking public services:

   ```sh
   GOPRIVATE=github.com/acme go install github.com/shaungsimpson/git-tui/cmd/git-tui@latest
   git-tui -version
   ```

   The last line should print the newest release, such as `git-tui v0.3.0`. On the way you may see:

   ```text
   go: downloading github.com/shaungsimpson/git-tui v0.3.0
   go: github.com/shaungsimpson/git-tui@v0.3.0 requires go >= 1.26.0; switching to go1.26.10
   go: downloading github.com/shaungsimpson/git-tui v0.3.0
   ```

   That is normal: your Go is older than the 1.26 the app needs, so Go downloads 1.26 once and uses it. The second `downloading` line is the newer Go fetching the module again.

   If it fails:

   - `404 Not Found` from `sum.golang.org`: Go asked a public service about a private repository. Set `GOPRIVATE` as above, or once with `go env -w GOPRIVATE=github.com/acme`.
   - `could not read Username for 'https://github.com'`: Git cannot log in to GitHub. Run `gh auth login` and then `gh auth setup-git`, or if you use SSH keys, `git config --global url."git@github.com:".insteadOf "https://github.com/"`.
   - `command not found: git-tui` after a successful install: add Go's install directory to your `PATH`. `go env GOBIN` prints it; if that is empty, it is `$(go env GOPATH)/bin`, usually `~/go/bin`.
   - It installs an older version: Go has not seen a release tagged in the last few minutes. Use `@v0.3.0` (or whichever version) instead of `@latest`, or try again shortly.

2. Look around with made-up data. Nothing is read or sent:

   ```sh
   git-tui -demo
   ```

   Press `?` for the shortcuts and `Ctrl+C` to quit.

3. For pull requests, environments, deploys and your profile header, log in to the GitHub CLI once with `gh auth login`. Without it, the local Git views still work.

4. Run it, then press `a` to add the repositories you work on:

   ```sh
   git-tui
   ```

   It looks for git repositories under a folder (`~/Code` to start with; change it as you like), and you press `Space` on each one to add, then `Enter`. Each is named after its GitHub repository, or its folder. [Managing repositories](#managing-repositories) has the rest, and [Configuration](#configuration) the file it writes.

To update later, run the install command again; `git-tui -version` shows what you have, and so does the Connections view. An installed release checks for a newer one about once a day and says so in the header; see [Updates](#updates).

## Updates

git-tui tells you when a newer stable release exists, and how to install it by hand. It never installs anything or changes your checkout.

- **Which builds are checked.** Go records how a binary was built. A stable version installed with `go install …@v0.5.0` (or `@latest`) is a release, checked against the GitHub Release marked latest about once a day. A build from a clone (`mise run install-cli`, `go build`), even one exactly on a release tag, is a source build. Anything else, such as `go install …@main`, is unknown. Source and unknown builds are never called out of date; `Ctrl+R` in Connections, or `git-tui -check-update`, shows the latest release on demand.
- **In the app.** When a newer release exists, an **Update v0.5.0** pill joins the badges at the right of the header (`[Update v0.5.0]` in the ASCII and text headers, the tab bar with the header off), so it reads without colour. The `git-tui` row in Connections shows the state (`up to date`, `update available`, `not checked`, `checking`, `check failed` or `not compared`), the version and kind of build, the executable, the latest stable release and how long ago it was checked, then what to do: the `go install` command pinned to the new version for a release; for a source build, `mise run update` on `main` (or update the checkout to another commit and rebuild); for an unknown build, both, since the app cannot tell which applies.
![Connections with the git-tui row selected: a release one version behind, the latest release and the go install command for it](docs/images/connections.png)

- **From the command line.** `git-tui -check-update` checks now, whatever the build, prints the same details and exits without starting the interface. It exits 0 when the check worked, whether or not there is a newer release, and 1 when it failed (after printing any earlier result). `-version` never checks.
- **How it checks.** One `gh api repos/shaungsimpson/git-tui/releases/latest` call (20-second timeout), so it needs `gh` logged in with access to the repository. Only a stable version (`vX.Y.Z`) is accepted. A successful check is cached in `$XDG_CACHE_HOME/git-tui/release.json` (default `~/.cache/git-tui/release.json`) and trusted for 24 hours across launches; a failed check is not cached, keeps the earlier result and its age on screen, and retries like any other load. A missing or unreadable cache only means checking again. Demo mode pretends to run v0.4.1 and finds v0.5.0, without the network or the cache.

## Prerequisites

- Go 1.26, or Go 1.21 or newer to fetch it: the module declares `go 1.26.0`, and with the default `GOTOOLCHAIN=auto` Go 1.21 and later download 1.26 automatically.
- `git` on `PATH`.
- Optional: the GitHub CLI (`gh`), logged in with `gh auth login`, for the Pull requests, Environments and Deploys views. Each repository's `origin` must point at github.com.
- A terminal at least 60×16. The sidebar appears from 100 columns and the details pane from 120 columns. When the list is narrower than 100 columns, commit, pull request and deploy rows put their title on a second line and everything else on the first, including the author when it fits; a long author follows the title instead of being cut off.

No Nerd Font or image support is required.

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

`~` is expanded. Each repository is validated on its own: a missing directory or a directory without `.git` is marked unavailable, and the app still opens. If no configuration file exists, the app opens and offers to add repositories, creating the file when you do. You can also write it by hand; start from `examples/config.example.json`.

## Managing repositories

The Connections view (`g c`) lists each configured repository under "Repository configuration". There:

| Key | Action |
|-----|--------|
| `a` | Add repositories (also offered from any view while none are configured) |
| `r` | Rename the selected repository. Names must be unique |
| `x` | Remove it from the list, after a `y` to confirm. The folder is not touched |
| `K` / `J` | Move it up or down; the sidebar and Overview follow this order |
| `f` | On a missing repository: find its folder again |

Adding looks for git repositories up to 3 folders below the folder you give, skipping hidden folders, `node_modules`, `vendor`, and the inside of a repository it found (so submodules are not listed). It follows links: a linked starting folder is searched, and a project linked into it is listed. A relative folder is taken from where you started git-tui and saved as a full path. It starts in the folder you used last, else the folder your repositories share, else `~/Code`. Repositories already configured are marked `[·]`. A name already in use gets its parent folder in front, such as `client-x/api`; rename it afterwards if you like. `f` looks the same way and puts the cursor on a folder with the repository's old folder name, or its name, and `Enter` points the repository there.

![Adding repositories: folders found under a starting folder, one already configured and marked [·], two chosen with Space](docs/images/add-repositories.png)

Each change is saved straight away. A repository keeps what it has already loaded when the list is reordered; a renamed one reloads its local Git data, which carries the name, but not its GitHub data.

You can configure two folders cloned from the same GitHub repository, such as a second checkout or a worktree on another branch. Each keeps its own branch, status, commits and diff. They share the GitHub data: pull requests, environments and deploys load once and are listed once, under the first folder's name, and the header counts them once. Paths under your home folder are written with `~`. The previous file is kept beside it as `config.json.bak`. If `config.json` is a link (into a dotfiles repository, say), the file it points to is written and the link stays, even when that file does not exist yet. If the file changed since git-tui loaded it, such as an edit by hand, nothing is saved and the status line asks you to restart, so your edit is not lost. Before anything is replaced, the new file is read back the way the app reads it at start-up, so a change from inside the app cannot leave a file it would reject. If the configuration failed to load (for example a typo in the JSON), these keys are not offered: fix the file by hand, so nothing you wrote is overwritten.

## Remembered preferences

The time zone (`u`), the profile header style (`b`) and the folder adding repositories last looked in are remembered between runs in `$XDG_STATE_HOME/git-tui/state.json`, which defaults to `~/.local/state/git-tui/state.json`. Besides this file, the app writes only the update check's cache (see [Updates](#updates)), and the configuration file and its `.bak` when you change the repository list. A `-profile` flag wins over the remembered header for that run. Demo mode neither reads nor saves preferences.

## Demo mode

`-demo` uses deterministic built-in fixtures. A `DEMO` badge appears in the header, and the status bar says no repositories are read. The fixtures include one repository that fails to read and one that is missing, so you can see the error and unavailable states. Demo mode shows every feature: the profile header with a generated avatar and every count lit, and PR pictures drawn from generated mock screenshots, all without touching the network. The paths it shows are fixtures too (repositories under `(demo)/`, `git` at `/usr/bin/git`, git-tui in `~/go/bin`), so screenshots of it never show your machine's folders or account name. `t`, `T` and `Ctrl+T` are offered in any terminal and say what they would open, without opening anything. Managing repositories works on the list in memory and saves nothing; adding finds fixture folders, including one whose name clashes and the folder the missing `infra` repository moved to. "This week" counts back from the fixtures' own date. Normal mode never falls back to fixtures.

## Views

![The Overview view: each repository's branch, worktree state, ahead/behind and latest commit](docs/images/overview.png)

| View        | Shows                                                                                   |
|-------------|-----------------------------------------------------------------------------------------|
| Overview    | Per repository: branch, worktree state, ahead/behind, latest commit                      |
| Activity    | Recent commits (100 per repository), merged by date, for all repositories or the one selected |
| Pull requests | Open PRs with review state, a ★ when your review is requested (directly or through a team), draft flag and combined check status (passing, pending, failing). Details show the description (first 200 lines, with its pictures) and the changed files; `d` shows the PR's code diff |
| Repository  | Branch, HEAD, upstream, tracking and worktree for the selected repository, plus its commits |
| Tags        | Tags; mark one as the base and pick another to list the **commits** between them        |
| Environments | Per environment, the newest successful GitHub deployment (commit, ref, time) and any newer failed or running attempt. Enter lists commits on `origin/HEAD` that the deployment does not contain |
| Deploys     | Recent deploy workflow runs: status, duration, branch and link. While a run is queued or running, the view reloads every 10 seconds by itself |
| Connections | What is available: local Git, the config and each configured repository (managed here, see [Managing repositories](#managing-repositories)), GitHub pull requests, deployments and deploy runs (from what those views last loaded), and git-tui itself: its version and kind of build, where it is installed, the latest release and how to update it (see [Updates](#updates)) |

In Pull requests, a green **✔** in the column after the check status marks your own PR that is ready to merge: approved, every check passing, and not a draft. Details says "Ready: ✔ approved, checks passing". Merge conflicts are not checked.

![Tags comparing v1.8.0 with v1.9.0: the commits between them, with the selected commit's details](docs/images/compare.png)

![Deploys across all repositories: a running deploy at the top, with successes and failures below](docs/images/deploys.png)

In Pull requests, a **★** in the column after the check status means someone has asked for your review, either you directly or a team you belong to. Details repeats it as "Requested: ★ your review". The star comes from GitHub's `review-requested:@me` search, so it disappears once you submit a review, and comes back if you are asked again. It sits apart from the review column, which shows the PR's overall state (approved, changes, review, draft). If the search fails, the list still loads without stars until the next refresh.

Authors are coloured so you can follow a person down a list: the same name always gets the same colour, ignoring case and punctuation (`Jane Doe` and `jane.doe` match). Your own name, as your GitHub login, display name or git name, is in the accent colour (a PR author must match your login exactly), and bots such as `renovate[bot]` and GitHub Apps (`app/dependabot`) are grey. A GitHub login and a git name for someone else can still get different colours, since nothing links them.

Tag comparisons are plain commit lists. PRs are squash-merged, so each commit title is already its PR title with `(#number)`; no feature summaries are invented. Environments show what **GitHub Deployments recorded**, not what the running service reports: a workflow can record success without the service serving that commit. "Not yet deployed" uses your local clone, so it is as current as your last `git fetch`; if the deployed commit is newer than your clone, the view says so. Ahead/behind reflects your last `git fetch`, since git-tui never fetches.

## Profile header

Above the tabs, a header shows who you are and what is waiting for you, across every configured repository: your GitHub name, handle, company, location and bio, review requests (★), your open PRs, how many of them are failing or have changes requested, your commits this week, repositories with uncommitted changes, followers, and your local git identity. It needs `gh` for the GitHub parts; without it, the header still shows your git identity and the local counts.

Two badges stand out at the right of the header's first line whenever there is something to act on:

- A red **🔔 3** badge counts the open PRs waiting on your review (the ★ ones), and a green **✔ 2** badge counts your PRs ready to merge. Counts above 9 show as `9+`. A badge is hidden at zero and until pull requests load; if a load fails, the last good count stays.
- In the ASCII and text headers they are drawn as `[! 3]` and `[ok 2]`. With the header off, or hidden because the window is short, they move to the tab bar, which drops the repository name, then the time zone, before the tabs to make room.
- When the review count goes up while the app is open, the red badge pulses for about three seconds and the status line says `Someone asked for your review. Press n to see it.` While reviews are waiting, the window title reads `git-tui (3)`, so you can see it from another tab.
- `n` lists the PRs behind them, and the footer shows `n reviews / ready to merge` while either badge is lit: see [Keyboard](#keyboard).

Press `b` to cycle it (the choice is remembered), or start with `-profile`:

| Mode    | Shows |
|---------|-------|
| `full`  | Your avatar beside a five-line banner (default) |
| `ascii` | The same banner in plain ASCII, the avatar drawn with characters |
| `text`  | One line of counts, zeros left out |
| `off`   | Nothing |

![The profile header in ASCII mode, its badges drawn as [! 3], [ok 2] and [Update v0.5.0]](docs/images/header-ascii.png)

![The profile header in text mode: one line of counts, with the same badges](docs/images/header-text.png)

The header degrades to what the terminal can show:

- Avatar: the real picture, cropped to a circle with an accent-coloured border, in Kitty and Ghostty (outside tmux), coloured half blocks in other terminals with 256 colours or more, characters with fewer colours or `NO_COLOR`, and your initials in a frame until it loads or when there is no picture.
- Height: the banner becomes the one-line header when it would leave the panes fewer than 10 lines, and is hidden when even that would. The footer's second line gives way before either.

## Keyboard

The footer lists the shortcuts that apply right now. When they do not fit on one line it takes a second, unless that would leave the panes fewer than 10 lines or shrink the profile header; then the others give way first so `?` help and `Ctrl+R` refresh stay visible. The footer keeps its height while you type a filter or answer a question, so the panes do not jump. `?` lists every key.

| Key               | Action                                                                     |
|-------------------|----------------------------------------------------------------------------|
| `j` `k` / `↓` `↑` | Move the selection in the focused pane                                     |
| `Tab` / `Shift+Tab` | Move focus between visible panes                                         |
| `Enter`           | Open or inspect the selected item (in Tags: choose the base tag, then the head tag; in Environments: list commits not yet deployed) |
| `Esc`             | Close details, the comparison, the base selection or the filter            |
| `/`               | Filter the current list (`Enter` applies, `Esc` cancels). In Pull requests, `is:review` keeps your review requests and `is:ready` your PRs ready to merge |
| `n`               | Go to Pull requests across all repositories, filtered to your review requests (`is:review`); again for your PRs ready to merge (`is:ready`); again to show every PR |
| `g` then `o` `a` `p` `r` `t` `e` `d` `c` | Go to Overview, Activity, Pull requests, Repository, Tags, Environments, Deploys or Connections (1 second timeout) |
| `Ctrl+R`          | Refresh everything on screen now, including failed loads; in Connections, also check for a newer release |
| `?`               | Contextual shortcut help                                                   |
| `F2`              | Toggle the dark and light themes                                           |
| `d`               | Show a diff: on a pull request, its code changes; on a repository with uncommitted changes (Overview row, sidebar or Repository view), those changes in sections for staged, unstaged and untracked. `j`/`k` scroll, `PgUp`/`PgDn` page, `Esc` closes |
| `o`               | In Pull requests: open the selected PR in your browser (https links only), with `open` on macOS, `wslview` on WSL when it is installed, and `xdg-open` otherwise |
| `t` / `T` / `Ctrl+T` | Open a new Ghostty tab, a split to the right, or a split below, at the selected repository's root (Overview row, sidebar or Repository view). Ghostty 1.4 or later on macOS, outside tmux; elsewhere the keys are hidden |
| `u`               | Show times in UTC or your local time zone (shown in the header)           |
| `b`               | Cycle the profile header: full, ASCII, text, off (always shown at the right of the footer) |
| `a` `r` `x` `K` `J` `f` | In Connections: add, rename, remove, reorder or find repositories. See [Managing repositories](#managing-repositories) |
| `Ctrl+C`          | Quit                                                                       |

![The diff of a pull request, opened with d](docs/images/diff.png)

The light theme (`F2`), and an 80×24 window, where the sidebar and details pane give way to the list:

![The Pull requests view in the light theme](docs/images/light.png)

![The Pull requests view in an 80×24 window, titles on a second line](docs/images/narrow.png)

While the filter, rename or folder input is open, all typing and pasted text go to the input, not to shortcuts.

## Scope and behaviour

- Git is run with argument arrays in each repository's directory, with a 5-second timeout. `GIT_TERMINAL_PROMPT=0` and `GIT_OPTIONAL_LOCKS=0` are set, so it never prompts and never refreshes the index, and `core.fsmonitor` is turned off. Repositories in the config are trusted: git may still run programs their own config names, such as clean filters (Git LFS is one), as it does in your shell.
- Only `status`, `log`, `for-each-ref`, `diff`, `ls-files` and `remote get-url` are run (adding repositories reads each found repository's `origin` with `remote get-url` to name it); `diff` uses `--no-ext-diff --no-textconv`, so a repository's own diff programs never run. There is no fetch, pull, checkout or tagging.
- GitHub calls go only to a remote whose host is github.com itself (or, over SSH, a host alias such as `github.com-work`).
- Pull requests are read with `gh pr list`, deployments with `gh api graphql` and deploy runs with `gh run list` (20-second timeout; up to 100 open PRs, 50 environments, the newest 20 deployments per environment and the newest 100 workflow runs).
- A deploy run is a run of a workflow named "deploy" (except pull request checks), or of the same workflow, trigger and branch as a run that recorded a GitHub deployment. That covers pipelines such as a main-branch CI/CD workflow without counting its pull request runs.
- Data on screen refreshes by itself when it is stale: local Git data after 5 seconds, GitHub data (pull requests, environments, deploys) after 1 minute, and your GitHub profile after 30 minutes. Staleness is checked on every key press, when you come back to the window, when it is resized, and on a 1-minute idle tick while the window is focused. The tick pauses while the window is in the background and after 10 minutes without a key press, so an open but unused window makes no `gh` calls. Every repository's pull requests and your GitHub profile always count as on screen, since the review and ready-to-merge badges show in every header mode. With the profile header shown, every repository's commits and status do too, since the header counts them; with it off, the rest refreshes only for the current view. The right of the tab bar says how old the data is (`Updated 3m ago`), or `Updating…` while a refresh runs; when the tabs need the room it shortens to `3m ago` (or `Just now`), then gives way.
- Deploys also reloads every 10 seconds while a deploy is queued or running. It stops when nothing is running, when you leave the view, or when a reload fails. Nothing is written to GitHub.
- Pictures in PR descriptions are drawn in the terminal in Ghostty and kitty (Kitty graphics protocol with Unicode placeholders), not inside tmux; other terminals, including WezTerm for now, show the picture's name instead. Only PNG, JPEG and GIF pictures hosted on GitHub are downloaded, up to 10 MB each. Your `gh` login token is sent to github.com only, because attachments in private repositories need it; it is not sent on to GitHub's storage host. Anything else, such as SVG or images on other sites, is named in the text instead. Demo mode never downloads pictures; it draws generated ones.
- A failed load keeps the last good data on screen and retries by itself after 1, 2, 4 and 8 minutes, then every 10 minutes; Ctrl+R retries at once, pictures and your avatar included, and the status line shows a `ctrl+r retry` hint.
- `t`, `T` and `Ctrl+T` run `osascript` with Ghostty's own AppleScript commands to open a tab in the front window, or split the focused pane (1-minute timeout, so there is time to answer the macOS permission prompt). A relative repository path is made absolute first. The repository path is passed as an argument, not written into the script. The first time, macOS may ask to let Ghostty control Ghostty; if you refuse, the key shows the error. Demo mode only says which directory it would open.
- Besides your repositories' pull requests, deployments and runs, the app reads your GitHub profile (`gh api user`), downloads PR pictures and your avatar from GitHub's hosts, and checks for a newer git-tui release (see [Updates](#updates)). Nothing else goes over the network.
- Loads run asynchronously. A refresh keeps the existing content visible, and results from superseded requests are discarded.
- Paths in your home folder (repositories, the configuration file, the executable, `git` and `gh`) are shown from `~`, so a screenshot does not give away your account name.
- Text from Git (subjects, branch names, errors) has its control characters, escape sequences and text direction overrides removed before display.

## Working on git-tui

[docs/development.md](docs/development.md) covers building, testing, the checks a pull request must pass, regenerating the screenshots (with the one-off macOS setup), releasing, and the code layout. [AGENTS.md](AGENTS.md) has the definition of done and the design principles, for people and coding agents alike.
