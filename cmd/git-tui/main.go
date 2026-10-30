// Command git-tui is a read-only terminal command centre for your repositories.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"
	"runtime/debug"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
	"github.com/shaungsimpson/git-tui/internal/ui"
)

func main() {
	cfgFlag := flag.String("config", "", "path to config JSON (default $GIT_TUI_CONFIG or ~/.config/git-tui/config.json)")
	demo := flag.Bool("demo", false, "use built-in fixture data instead of real repositories")
	version := flag.Bool("version", false, "print the version and exit")
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
	opts := ui.Options{Demo: *demo, Images: termimg.Supported(os.Getenv), Profile: profile, UTC: state.UTC,
		StatePath: statePath, Version: v, Tools: map[string]string{}}
	opts.Executable, _ = os.Executable()
	for _, t := range []string{"git", "gh"} {
		if p, err := exec.LookPath(t); err == nil {
			opts.Tools[t] = p
		}
	}

	if *demo {
		opts.Source = gitx.Demo{}
		for _, name := range gitx.DemoRepos {
			opts.Repos = append(opts.Repos, config.Repo{Name: name, Path: "(demo)/" + name, Status: config.StatusOK})
		}
		opts.Repos = append(opts.Repos, config.Repo{Name: "infra", Path: "(demo)/infra", Status: config.StatusMissing, Err: "directory not found: (demo)/infra"})
	} else {
		opts.Source = gitx.Git{}
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
