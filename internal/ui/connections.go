package ui

import (
	"errors"
	"fmt"
	"path/filepath"

	"charm.land/lipgloss/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

type level int

const (
	levelOK level = iota
	levelWarn
	levelOff
)

type connRow struct {
	glyph, name, state string
	level              level
	detail             []string
}

func (m *Model) levelStyle(l level) lipgloss.Style {
	switch l {
	case levelOK:
		return m.st.ok
	case levelWarn:
		return m.st.warn
	}
	return m.st.muted
}

func (m *Model) tool(name string) (string, bool) {
	p := m.opts.Tools[name]
	return p, p != ""
}

// connections describes what the app can and cannot see. Nothing here contacts a remote service.
func (m *Model) connections() []connRow {
	var rows []connRow

	if p, ok := m.tool("git"); ok {
		rows = append(rows, connRow{"·", "Local Git", "available", levelOK, []string{
			"Executable: " + clean(config.Tilde(p)),
			"Reads branch, HEAD, worktree, tracking, commits and tags with read-only git commands.",
			"Nothing is fetched, so ahead/behind reflects the last fetch you ran yourself.",
		}})
	} else {
		rows = append(rows, connRow{"!", "Local Git", "git not found", levelWarn, []string{
			"Install git and make sure it is on PATH, then restart git-tui.",
		}})
	}

	switch {
	case m.opts.Demo:
		rows = append(rows, connRow{"·", "Repository configuration", "demo fixtures", levelWarn, []string{
			"Demo mode uses built-in fixture data. No configuration file or repository is read.",
		}})
	case errors.Is(m.opts.ConfigErr, config.ErrNotFound):
		rows = append(rows, connRow{"!", "Repository configuration", "no file yet", levelWarn, []string{
			"Path: " + clean(config.Tilde(m.opts.ConfigPath)),
			"Press a to add repositories; the file is created when you do.",
		}})
	case m.opts.ConfigErr != nil:
		rows = append(rows, connRow{"!", "Repository configuration", "not loaded", levelWarn, []string{
			"Path: " + clean(config.Tilde(m.opts.ConfigPath)),
			"Error: " + clean(m.opts.ConfigErr.Error()),
			"Fix the file by hand: git-tui does not change a configuration it cannot read.",
		}})
	default:
		bad := 0
		for _, r := range m.repos {
			if r.cfg.Status != config.StatusOK {
				bad++
			}
		}
		row := connRow{"·", "Repository configuration", fmt.Sprintf("%d repositories", len(m.repos)), levelOK, []string{
			"Path: " + clean(config.Tilde(m.opts.ConfigPath)),
			"Press a to add repositories. Changes are saved at once; the previous file is kept as " + clean(filepath.Base(m.opts.ConfigPath)) + ".bak.",
		}}
		if bad > 0 {
			row.glyph, row.level, row.state = "!", levelWarn, fmt.Sprintf("%d of %d unavailable", bad, len(m.repos))
		}
		rows = append(rows, row)
	}
	// One row per repository follows the configuration row; repoRows relies on that order.
	for _, r := range m.repos {
		row := connRow{"·", "  " + clean(r.cfg.Name), "ok", levelOK, []string{"Path: " + clean(config.Tilde(r.cfg.Path)), ""}}
		if r.cfg.Status != config.StatusOK {
			row.glyph, row.state, row.level = "!", r.cfg.Status.String(), levelWarn
			row.detail = append(row.detail, clean(r.cfg.Err), "f finds a moved folder.", "")
		}
		row.detail = append(row.detail, "r renames, x removes, K and J move it up and down.")
		rows = append(rows, row)
	}

	rows = append(rows,
		m.githubRow("GitHub (pull requests)", "Pull requests (g p)", "open pull requests with `gh pr list`",
			func(r *repoState) (bool, error) { return r.gh.prs.loaded, r.gh.prs.err }),
		m.githubRow("GitHub (deployments)", "Environments (g e)", "GitHub Deployments records with `gh api` (not verified against running services)",
			func(r *repoState) (bool, error) { return r.gh.deps.loaded, r.gh.deps.err }),
		m.githubRow("GitHub (deploy runs)", "Deploys (g d)", "deploy workflow runs with `gh run list`",
			func(r *repoState) (bool, error) { return r.gh.runs.loaded, r.gh.runs.err }))

	rows = append(rows, m.releaseRow())
	return rows
}

// githubRow reports one gh-backed source from what its view has actually loaded; nothing is checked from here.
func (m *Model) githubRow(name, view, what string, state func(*repoState) (bool, error)) connRow {
	p, ok := m.tool("gh")
	if !ok && !m.opts.Demo {
		return connRow{"!", name, "gh not found", levelWarn, []string{
			"This is read with the GitHub CLI.",
			"Install it (brew install gh), run `gh auth login`, then press Ctrl+R in " + view + ".",
		}}
	}
	loaded := 0
	var errs []string
	seen := map[*ghState]bool{} // clones share one load, so it counts once
	for i := range m.repos {
		if seen[m.repos[i].gh] {
			continue
		}
		seen[m.repos[i].gh] = true
		done, err := state(m.repos[i])
		if done {
			loaded++
		}
		if err != nil && !errors.Is(err, gitx.ErrNoOrigin) { // not pushed yet is not a gh failure
			errs = append(errs, clean(m.repos[i].cfg.Name)+": "+clean(err.Error()))
		}
	}
	source := "gh found at " + clean(config.Tilde(p)) + "."
	if m.opts.Demo {
		source = "Demo mode: this data is fixtures."
	}
	switch {
	case len(errs) > 0:
		detail := append([]string{source, "Some repositories could not be read:", ""}, errs...)
		return connRow{"!", name, fmt.Sprintf("%d failed", len(errs)), levelWarn, append(detail, "",
			"Check `gh auth status` and that origin points at github.com, then press Ctrl+R in "+view+".")}
	case loaded > 0:
		return connRow{"·", name, "available", levelOK, []string{source,
			fmt.Sprintf("Read %s for %d repositories (read-only).", what, loaded)}}
	}
	return connRow{"-", name, "not checked yet", levelOff, []string{source, "Open " + view + " to read it."}}
}

// repoRows is where the per-repository rows start in connections().
func repoRows(rows []connRow) int {
	for i, r := range rows {
		if r.name == "Repository configuration" {
			return i + 1
		}
	}
	return len(rows)
}
