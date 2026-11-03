// Command git-tui is a read-only terminal command centre for your repositories.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
	"github.com/shaungsimpson/git-tui/internal/ui"
	"github.com/shaungsimpson/git-tui/internal/update"
)

func main() {
	cfgFlag := flag.String("config", "", "path to config JSON (default $GIT_TUI_CONFIG or ~/.config/git-tui/config.json)")
	demo := flag.Bool("demo", false, "use built-in fixture data instead of real repositories")
	version := flag.Bool("version", false, "print the version and exit")
	checkUpdate := flag.Bool("check-update", false, "check GitHub for a newer stable release, print how to update, and exit")
	profileFlag := flag.String("profile", "full", "profile header: full, ascii, text or off (b cycles them)")
	flag.Parse()

	// Go stamps the version from the git tag at build time, e.g. v0.1.0, or a pseudo-version between tags.
	v := "(devel)"
	if info, ok := debug.ReadBuildInfo(); ok && info.Main.Version != "" {
		v = info.Main.Version
	}
	if *version {
		fmt.Println("git-tui", v)
		return
	}
	exe, _ := os.Executable()
	build, src, cache := update.Current(), gitx.Source(gitx.Git{}), update.CachePath()
	if *demo {
		build, src, cache, v = gitx.DemoBuild, gitx.Demo{}, "", gitx.DemoBuild.Version // help and Connections agree
		exe = "~/go/bin/git-tui"                                                       // a fixture, like the tool paths below
	}
	if *checkUpdate {
		os.Exit(checkForUpdate(build, src, cache, exe))
	}

	// Toggles are remembered between runs, except in demo mode; a -profile flag wins over the saved header.
	var state config.State
	statePath := ""
	if !*demo {
		statePath = config.StatePath()
		state, _ = config.LoadState(statePath) // an unreadable file only loses the preferences
	}
	profile, ok := ui.ParseProfile(*profileFlag)
	if !ok {
		fmt.Fprintln(os.Stderr, "git-tui: -profile must be full, ascii, text or off")
		os.Exit(2)
	}
	flagged := false
	flag.Visit(func(f *flag.Flag) { flagged = flagged || f.Name == "profile" })
	if saved, ok := ui.ParseProfile(state.Profile); ok && !flagged {
		profile = saved
	}

	// Demo mode never touches the network: its avatar and PR pictures are drawn from fixtures.
	opts := ui.Options{Demo: *demo, Images: termimg.Supported(os.Getenv),
		Tabs: ui.CanOpenTabs(os.Getenv), Profile: profile, UTC: state.UTC, NoPreview: state.NoPreview, Dates: state.Dates,
		StatePath: statePath, Sorts: state.Sorts, Group: state.Group, Version: v, Tools: map[string]string{}, Executable: exe, Build: build, Source: src, ReleaseCache: cache}
	if cache != "" {
		opts.Release = update.LoadCache(cache, time.Now()) // an unreadable cache only means checking again
	}
	if *demo { // fixtures, so the demo and its screenshots never show this machine's paths
		opts.Tools = map[string]string{"git": "/usr/bin/git", "gh": "/usr/bin/gh"}
		for _, name := range gitx.DemoRepos {
			opts.Repos = append(opts.Repos, config.Repo{Name: name, Path: "(demo)/" + name, Group: gitx.DemoGroups[name], Status: config.StatusOK})
		}
		opts.Repos = append(opts.Repos, config.Repo{Name: "infra", Path: "(demo)/infra", Group: gitx.DemoGroups["infra"], Status: config.StatusMissing, Err: "directory not found: (demo)/infra"})
	} else {
		for _, t := range []string{"git", "gh"} {
			if p, err := exec.LookPath(t); err == nil {
				opts.Tools[t] = p
			}
		}
		opts.ConfigPath = config.Resolve(*cfgFlag)
		// Config errors are shown inside the UI (Connections and empty states) rather than aborting.
		cfg, err := config.Load(opts.ConfigPath)
		opts.Repos, opts.ConfigErr = cfg.Repositories, err
	}

	if _, err := tea.NewProgram(ui.New(opts)).Run(); err != nil {
		fmt.Fprintln(os.Stderr, "git-tui:", err)
		os.Exit(1)
	}
}

// checkForUpdate asks GitHub now, whatever the build, and prints the result with how to update by hand. A newer
// release is a successful check; only a failed one exits nonzero, after showing any earlier result from the cache.
func checkForUpdate(build update.Build, src update.Releases, cache, exe string) int {
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
	defer cancel()
	l, err := update.Check(ctx, src, cache, time.Now())
	if err != nil && cache != "" {
		l = update.LoadCache(cache, time.Now())
	}
	fmt.Println(strings.Join(ui.UpdateReport(build, exe, l, err, time.Now()), "\n"))
	if err != nil {
		return 1
	}
	return 0
}
