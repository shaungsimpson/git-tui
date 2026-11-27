package ui

import (
	"time"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/update"
)

// updateState is what the app knows about a newer release of itself.
func (m *Model) updateState() update.State {
	return update.Assess(m.opts.Build, m.release.val, m.release.loading, m.release.err)
}

// releaseRow is the Connections row for git-tui itself.
func (m *Model) releaseRow() connRow {
	st := m.updateState()
	row := connRow{"·", "git-tui", st.String(), levelOK, releaseLines(m.opts.Build, m.opts.Executable, m.release.val, st, m.release.err, clock())}
	if st == update.Available || st == update.Unavailable {
		row.glyph, row.level = "!", levelWarn
	}
	row.detail = append(row.detail, "", "Ctrl+R here checks for the latest stable release now.")
	return row
}

// UpdateReport is what -check-update prints: the state, then the details Connections shows.
func UpdateReport(b update.Build, exe string, l update.Latest, err error, now time.Time) []string {
	st := update.Assess(b, l, false, err)
	return append([]string{"git-tui: " + st.String()}, releaseLines(b, exe, l, st, err, now)...)
}

// releaseLines describes the running build, the latest stable release and how to update by hand. Nothing is
// run for the user: how the binary was installed is not something the build can prove.
func releaseLines(b update.Build, exe string, l update.Latest, st update.State, err error, now time.Time) []string {
	kind := "unknown build"
	switch b.Kind {
	case update.Release:
		kind = "release"
	case update.Source:
		kind = "source build"
		if b.Revision != "" {
			kind += " of " + clean(b.Revision[:min(12, len(b.Revision))])
		}
		if b.Dirty {
			kind += " with uncommitted changes"
		}
	}
	lines := []string{"Version: " + clean(firstOf(b.Version, "unknown")) + " (" + kind + ")", "Executable: " + clean(firstOf(config.Tilde(exe), "unknown"))}
	switch {
	case l.Version != "":
		lines = append(lines, "Latest stable release: "+l.Version+", checked "+ago(now.Sub(l.Checked))+".")
	case st == update.Checking:
		lines = append(lines, "Latest stable release: checking…")
	default:
		lines = append(lines, "Latest stable release: not checked yet.")
	}
	if err != nil {
		lines = append(lines, "The last check failed: "+clean(err.Error()))
	}
	lines = append(lines, "")
	install := "GOPRIVATE=" + update.Module + " go install " + update.Module + "/cmd/git-tui@" + l.Version
	rebuild := "in the checkout, run mise run update on main, or check out the commit you want and run mise run install-cli (or go install ./cmd/git-tui)."
	switch {
	case st == update.Available:
		lines = append(lines, "To install "+l.Version+", run:", "  "+install)
	case st == update.UpToDate && err != nil: // the result is the last successful check's, not this one's
		lines = append(lines, "No newer release at the last successful check.")
	case st == update.UpToDate:
		lines = append(lines, "This is the latest stable release.")
	case b.Kind == update.Release && st != update.Checking: // nothing known yet, usually because gh cannot be used
		lines = append(lines, "Check that gh is installed and logged in (gh auth status), then press Ctrl+R in Connections or run git-tui -check-update.")
	case b.Kind == update.Source:
		lines = append(lines, "Built from a checkout, so it is not compared with releases.", "To rebuild: "+rebuild)
	case b.Kind == update.Unknown:
		lines = append(lines, "How this binary was built is unknown, so it is not compared with releases.", "Update it the way you installed it:")
		if l.Version != "" {
			lines = append(lines, "  With go install: "+install)
		}
		lines = append(lines, "  From a checkout: "+rebuild)
	}
	return lines
}
