# Developing git-tui

For working on the code. To install and use the app, see the [README](../../README.md#get-started). [AGENTS.md](../../AGENTS.md) has the definition of done for a pull request and the design principles to follow, for people and coding agents alike.

- This page: setting up, tasks, the checks before a pull request, running your own build, and the code layout.
- [Screenshots](screenshots.md): regenerating the README's pictures, with the one-off macOS setup.
- [Releasing](releasing.md): tagging a version and publishing its GitHub Release.

## Set up

You need Go (see the README's [Prerequisites](../../README.md#prerequisites)), `git`, and [mise](https://mise.jdx.dev), which runs the tasks below. Clone the repository, then on first use in the folder:

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
mise run update             # on main: pulls (fast-forward only) and installs the git-tui command
mise run screenshots        # regenerates docs/images (macOS and Ghostty; see screenshots.md)
```

## Before a pull request

```sh
mise run lint && mise run test && mise run build
TZ=UTC mise run test        # CI runs in UTC; catches times and layouts that only pass in your own zone
go test ./internal/ui -run x -bench ManyRepos   # frame and load cost with 30 repositories; run after touching layout or lists
```

CI (`.github/workflows/ci.yml`) runs the same lint and test checks plus a build on every pull request to `main` and every push to `main`. Tests use fixtures and temporary Git repositories; they never touch the network or a real terminal.

Demo mode is the showcase, so it must show every feature. When you add one, give the demo fixtures (`internal/gitx/demo.go`, `demo_images.go`) what it needs and extend `TestDemoShowsEveryFeature`. When the screen visibly changes, regenerate the pictures ([Screenshots](screenshots.md)). The pull request template has the full checklist.

## Your own build

After `mise run install-cli`, run `git-tui` from anywhere (Go's install directory must be on your `PATH`: `go env GOBIN`, or `$(go env GOPATH)/bin` if that is empty, usually `~/go/bin`). Run it again after pulling changes, or run `mise run update` on `main` to do both: it pulls with `--ff-only`, so it never makes a merge commit, and stops if you are on another branch.

`git-tui -version` prints the version it was built from; the app also shows it in the Connections view and on the `?` help screen. A build from a clone shows a pre-release version such as `v0.3.1-0.20261030…` (Go 1.24 and later record it from the commit), the tag when you are exactly on a release, and `+dirty` on the end when there are uncommitted changes.

`mise run install-cli` and the README's install command write the same `git-tui` executable to the same directory, so each replaces the other: the last one you ran is the one you have. To go back to the release after testing your own build, run the install command from the README again; to come back to your own build, `mise run install-cli` (or `mise run update` on `main`). `go version -m "$(which git-tui)"` tells them apart: a release has an `h1:` checksum on its `mod` line, and a build from a clone has a `vcs.revision` line instead. The update check reads the same metadata (`internal/update`), so your own build is a source build and never shows the update badge; `git-tui -check-update` still tells you the latest release. There is no mise task for the release install on purpose: mise only runs inside a clone, and the release install is for when you do not need one.

## Layout

```
cmd/git-tui        entry point and flags
internal/config      JSON config loading, saving (Save) and per-repository validation, the repository scan (Scan), remembered preferences
internal/gitx        read-only git and gh runner, parsers, demo fixtures (demo.go, demo_images.go)
internal/update      which build is running (Go build metadata), version comparison, the update check and its cache
internal/ui          Bubble Tea model and key dispatch (model.go), asynchronous loads (load.go), list contents (lists.go),
                     diff and commit comparisons (compare.go), PR descriptions as markdown (markdown.go, with goldmark),
                     PR pictures (pictures.go), browser and terminal tabs (open.go), rendering (view.go), key table (keys.go), profile header (profile.go), refresh cadence (refresh.go),
                     managing repositories (repos.go), groups (groups.go), the Connections view (connections.go), colours (theme.go),
                     review, ready-to-merge and update badges (alerts.go), the update details (update.go);
                     tests sit beside the code they cover (load_test.go for load.go), with shared helpers in helpers_test.go
internal/termimg     picture downloads, Kitty graphics, colour-block and character drawing (art.go)
scripts              screenshots.sh, behind mise run screenshots
docs/development     this guide, screenshots.md and releasing.md
docs                 the README's pictures (images/)
```
