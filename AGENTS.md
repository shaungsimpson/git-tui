# Working on git-tui

Standards for anyone changing this repository, people and coding agents alike. The README describes the app; this file is how changes are made.

## Definition of done

A pull request is ready when all of these hold. CI checks the first two; reviewers (CodeRabbit included) check the rest.

1. **Lint passes:** `mise run lint` (golangci-lint with `.golangci.yml`, which includes gofmt and vet).
2. **Tests pass:** `mise run test`. New logic (a branch, a loop, a parser, anything touching security or the network) comes with a test. Tests never touch the network or a real terminal: use `gitx.Demo`, a fake `Source`, or the package variables such as `fetchPic` and `openBrowser`.
3. **Docs match the change.** Update the README in the same pull request for anything a user can see or do: the Get started steps, flags, the Views and Keyboard tables, Scope and behaviour, the Layout tree, and files the app reads or writes.
4. **Demo shows it.** Give the demo fixtures (`internal/gitx/demo.go`, `demo_images.go`) what the feature needs to appear, and extend `TestDemoShowsEveryFeature`. Demo is how the tool is shown without real data, so a feature missing from demo is invisible.
5. **Screenshots are current.** When the screen visibly changes, run `mise run screenshots` (macOS with Ghostty; see the README) and commit `docs/images/`. If you cannot run it, say so in the pull request.
6. **The pull request says what was not checked**, such as a terminal you could not try or a path that needs real GitHub data.

## Design principles

- **Read-only.** The app never changes a repository or GitHub. Git runs only the commands listed in the README's Scope and behaviour, and never fetches. The only file it writes is its own preferences file (`internal/config/state.go`).
- **Network only through GitHub.** Data comes from `gh`. Pictures come only from hosts in `termimg.Allowed`, and the login token goes to github.com only. Demo mode makes no network calls.
- **External text is untrusted.** Pass anything from Git, GitHub or a config file through `clean()` before it is drawn, and only open https links.
- **External commands are bounded.** Argument arrays, never a shell string; every call has a timeout (`command` in `internal/gitx`).
- **Loads are asynchronous and superseded results are dropped.** Use the `slot`, `start` and `finish` pattern in `internal/ui/model.go`. A failed load keeps the old content and retries with a backoff (1, 2, 4, 8, then every 10 minutes), or at once on Ctrl+R.
- **Degrade gracefully.** Detect what the terminal and environment can do and fall back step by step: a Kitty picture, then colour blocks, then characters, then text; panes that collapse as the window narrows; working local views without `gh` or a config file. Never crash or show a blank screen because something is missing.
- **One source of truth for keys.** Every shortcut is a row in the `actions` table in `internal/ui/keys.go`, which drives dispatch, help and the footer.
- **Refresh only what is on screen and stale.** `wanted` in `internal/ui/refresh.go` lists the data on screen, and `ageOf` sets how long each kind stays fresh. New data joins that list and picks a tier; do not add timers of your own.
- **Every frame fits.** Rendered lines never exceed the terminal width or height; `TestRenderFitsTerminal` checks the common sizes.

## Code and writing style

- Make the smallest change that fully solves the task. Reuse what the code already has before adding anything; prefer the standard library, and add a dependency only when a few lines cannot do the job.
- Match the surrounding code: its naming, its comment density, its idioms. Comments explain why, in one line, not what.
- A deliberate limitation gets a comment in the form `// shortcut: <the limit>, <when to upgrade>`.
- Australian spelling in comments, docs and UI text (colour, behaviour, centre). Library identifiers such as `color.RGBA` keep their own spelling.
- UI text is in sentence case: capitalise the first word and proper nouns only.

## Git and pull requests

- Never push to `main`. Work on a branch and open a pull request; it is squash-merged.
- Commits and pull request titles follow [Conventional Commits](https://www.conventionalcommits.org) (`feat:`, `fix:`, `docs:`, `chore:`, `perf:`, `test:`), with no emojis.
- Releases are git tags on `main` (`v0.3.0`), made after the merge. There is no version file to bump.
