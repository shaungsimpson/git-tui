package ui

import (
	"fmt"

	"charm.land/lipgloss/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
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
			"Executable: " + clean(p),
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
	case m.opts.ConfigErr != nil:
		rows = append(rows, connRow{"!", "Repository configuration", "not loaded", levelWarn, []string{
			"Path: " + clean(m.opts.ConfigPath),
			"Error: " + clean(m.opts.ConfigErr.Error()),
			"Create the file from examples/config.example.json or pass -config FILE.",
		}})
	default:
		bad := 0
		detail := []string{"Path: " + clean(m.opts.ConfigPath), ""}
		for _, r := range m.repos {
			state := "ok"
			if r.cfg.Status != config.StatusOK {
				bad++
				state = clean(r.cfg.Err)
			}
			detail = append(detail, clean(r.cfg.Name)+": "+state)
		}
		row := connRow{"·", "Repository configuration", fmt.Sprintf("%d repositories", len(m.repos)), levelOK, detail}
		if bad > 0 {
			row.glyph, row.level, row.state = "!", levelWarn, fmt.Sprintf("%d of %d unavailable", bad, len(m.repos))
		}
		rows = append(rows, row)
	}

	rows = append(rows,
		m.githubRow("GitHub (pull requests)", "Pull requests (g p)", "open pull requests with `gh pr list`",
			func(r *repoState) (bool, error) { return r.prs.loaded, r.prs.err }),
		m.githubRow("GitHub (deployments)", "Environments (g e)", "GitHub Deployments records with `gh api` (not verified against running services)",
			func(r *repoState) (bool, error) { return r.deps.loaded, r.deps.err }),
		m.githubRow("GitHub (deploy runs)", "Deploys (g d)", "deploy workflow runs with `gh run list`",
			func(r *repoState) (bool, error) { return r.runs.loaded, r.runs.err }))

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
	for i := range m.repos {
		done, err := state(&m.repos[i])
		if done {
			loaded++
		}
		if err != nil {
			errs = append(errs, clean(m.repos[i].cfg.Name)+": "+clean(err.Error()))
		}
	}
	source := "gh found at " + clean(p) + "."
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
