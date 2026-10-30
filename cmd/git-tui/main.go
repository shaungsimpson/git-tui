// Command git-tui is a read-only terminal command centre for your repositories.
package main

import (
	"flag"
	"fmt"
	"os"
	"os/exec"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/ui"
)

func main() {
	cfgFlag := flag.String("config", "", "path to config JSON (default $GIT_TUI_CONFIG or ~/.config/git-tui/config.json)")
	demo := flag.Bool("demo", false, "use built-in fixture data instead of real repositories")
	flag.Parse()

	opts := ui.Options{Demo: *demo, Tools: map[string]string{}}
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
