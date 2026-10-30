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

	rows = append(rows, m.githubRow())

	rows = append(rows, connRow{"-", "Deployment environments", "not configured", levelOff, []string{
		"Deployed revisions are not shown because no source of truth is configured.",
		"Nothing here is inferred from tags or branches.",
	}})
	return rows
}

// githubRow reports gh from what the Pull requests view has actually loaded; nothing is checked from here.
func (m *Model) githubRow() connRow {
	const name = "GitHub (pull requests)"
	p, ok := m.tool("gh")
	if !ok && !m.opts.Demo {
		return connRow{"!", name, "gh not found", levelWarn, []string{
			"Pull requests are read with the GitHub CLI.",
			"Install it (brew install gh), run `gh auth login`, then press Ctrl+R in Pull requests.",
		}}
	}
	loaded := 0
	var errs []string
	for _, r := range m.repos {
		if r.prs.loaded {
			loaded++
		}
		if r.prs.err != nil {
			errs = append(errs, clean(r.cfg.Name)+": "+clean(r.prs.err.Error()))
		}
	}
	source := "gh found at " + clean(p) + "."
	if m.opts.Demo {
		source = "Demo mode: pull requests are fixtures."
	}
	switch {
	case len(errs) > 0:
		detail := append([]string{source, "Some repositories could not be read:", ""}, errs...)
		return connRow{"!", name, fmt.Sprintf("%d failed", len(errs)), levelWarn, append(detail, "",
			"Check `gh auth status` and that origin points at github.com, then press Ctrl+R in Pull requests.")}
	case loaded > 0:
		return connRow{"·", name, "available", levelOK, []string{source,
			fmt.Sprintf("Open pull requests read for %d repositories with `gh pr list` (read-only).", loaded)}}
	}
	return connRow{"-", name, "not checked yet", levelOff, []string{source,
		"Open the Pull requests view (g p) to read open pull requests and their checks."}}
}
