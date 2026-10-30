# Developing git-tui

For working on the code. To install and use the app, see the [README](../README.md#get-started). [AGENTS.md](../AGENTS.md) has the definition of done for a pull request and the design principles to follow, for people and coding agents alike.

## Set up

You need Go (see the README's [Prerequisites](../README.md#prerequisites)), `git`, and [mise](https://mise.jdx.dev), which runs the tasks below. Clone the repository, then on first use in the folder:

```sh
mise trust
```

mise installs what a task needs on first run, such as golangci-lint for `mise run lint`.

## Tasks

```sh
mise tasks                  # list tasks
mise run build              # builds bin/git-tui
mise run lint               # golangci-lint (includes gofmt and go vet)
mise run test               # gofmt check, go vet, tests with the race detector
mise run demo               # builds and runs with fixture data, no repositories read
mise run start              # builds and runs with the default config location
mise run start -- -config examples/config.example.json
mise run install-cli        # installs the git-tui command into Go's install directory
mise run screenshots        # regenerates docs/images (macOS and Ghostty; see Screenshots)
```

## Before a pull request

```sh
mise run lint && mise run test && mise run build
```

CI (`.github/workflows/ci.yml`) runs the same lint and test checks plus a build on every pull request to `main` and every push to `main`. Tests use fixtures and temporary Git repositories; they never touch the network or a real terminal.

Demo mode is the showcase, so it must show every feature. When you add one, give the demo fixtures (`internal/gitx/demo.go`, `demo_images.go`) what it needs and extend `TestDemoShowsEveryFeature`. The pull request template has the full checklist.

## Your own build

After `mise run install-cli`, run `git-tui` from anywhere (Go's install directory must be on your `PATH`: `go env GOBIN`, or `$(go env GOPATH)/bin` if that is empty, usually `~/go/bin`). Run it again after pulling changes.

`git-tui -version` prints the version it was built from; the app also shows it in the Connections view and on the `?` help screen. A build from a clone shows a pre-release version such as `v0.3.1-0.20261030…` (Go 1.24 and later record it from the commit), the tag when you are exactly on a release, and `+dirty` on the end when there are uncommitted changes.

`mise run install-cli` and the README's install command write the same `git-tui` executable to the same directory, so each replaces the other: the last one you ran is the one you have. To go back to the release after testing your own build, run the install command from the README again. `go version -m "$(which git-tui)"` tells them apart: a release has an `h1:` checksum on its `mod` line, and a build from a clone has a `vcs.revision` line instead. There is no mise task for the release install on purpose: mise only runs inside a clone, and the release install is for when you do not need one.

## Screenshots

`mise run screenshots` (`scripts/screenshots.sh`) regenerates the pictures in `docs/images/` that the README shows. It runs `git-tui -demo` in a real Ghostty window, so the avatar and PR pictures are drawn exactly as users see them, presses keys to reach each view, and captures the window. Run it whenever a change alters what the screen looks like, and commit `docs/images/`.

It cannot run in CI: it needs a Mac with a screen.

### One-off setup

1. **macOS and Ghostty.** Install [Ghostty](https://ghostty.org) in `/Applications`. The script opens its own Ghostty window with Ghostty's default settings, so your Ghostty configuration does not change the pictures. Everything else it uses (`osascript`, `screencapture`, `sips`) comes with macOS.
2. **Screen Recording,** so the script can capture the window. Open System Settings › Privacy & Security › Screen Recording, and turn it on for the app you run the script from: your terminal (Ghostty, Terminal, iTerm2) or your editor if you use its terminal. Quit and reopen that app afterwards.
3. **Accessibility,** so the script can press keys in its window. In System Settings › Privacy & Security › Accessibility, turn it on for the same app.
4. **Automation,** on the first run. macOS asks whether your terminal may control System Events. Allow it. If you said no, turn it on in System Settings › Privacy & Security › Automation, under your terminal.

### Running it

```sh
mise run screenshots
```

A Ghostty window opens, steps through the views and closes, then a narrower one does the same: about 20 seconds in all. **Leave the keyboard and mouse alone while it runs.** Keystrokes go to whichever app is in front, so the script brings its window forward and checks it is in front before every key press; if anything else takes the front, it stops instead of typing into it. It prints each picture it writes:

```text
docs/images/overview.png
docs/images/pull-requests.png
docs/images/header-ascii.png
docs/images/header-text.png
docs/images/light.png
docs/images/narrow.png
```

Look at the pictures before committing them: each should show a different screen.

### When it fails

| Message or symptom | Cause and fix |
|---|---|
| `needs macOS and /Applications/Ghostty.app` | Install Ghostty in `/Applications`. |
| `Ghostty window did not appear` | Ghostty did not open a window within 10 seconds. Run it again; if it keeps happening, open Ghostty once by hand to clear any first-launch prompt. |
| `The screenshot window is not in front, so no keys were sent` | Something else took the front: you clicked or typed, or a notification appeared. Run it again without touching anything. |
| `could not create image from window`, or pictures of only the desktop | Screen Recording is off for the app running the script. Turn it on (step 2) and reopen that app. |
| `osascript is not allowed to send keystrokes` | Accessibility is off for the app running the script (step 3). |
| `Not authorized to send Apple events to System Events` (macOS's spelling) | Automation was refused (step 4). |
| Every picture shows the first screen | The keys did not reach the window. Check Accessibility, then run it again hands off. |

## Releasing

Releases are git tags on `main`, made after a pull request is squash-merged; there is no version file to bump.

```sh
git switch main && git pull
git tag v0.4.0 && git push origin v0.4.0
```

A new feature bumps the middle number while the version is below 1.0 (`v0.3.0` to `v0.4.0`); a fix alone bumps the last (`v0.3.1`). Users get it with the README's install command.

## Layout

```
cmd/git-tui        entry point and flags
internal/config      JSON config loading and per-repository validation, remembered preferences
internal/gitx        read-only git and gh runner, parsers, demo fixtures
internal/ui          Bubble Tea model (model.go), rendering (view.go), key table (keys.go), profile header (profile.go), refresh cadence (refresh.go)
internal/termimg     picture downloads, Kitty graphics, colour-block and character drawing
scripts              screenshots.sh, behind mise run screenshots
docs                 this guide and the README's pictures (images/)
```
