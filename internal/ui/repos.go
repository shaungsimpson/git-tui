package ui

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

// scanDepth is how many folders down from the chosen folder adding repositories looks.
const scanDepth = 3

// found is one git repository a scan turned up, with the name it would be added under.
type found struct {
	path, name string
}

// scanState is the full-width screen that adds repositories, or finds a moved one again (fix >= 0).
type scanState struct {
	open   bool
	fix    int // the repository being pointed at a new folder, or -1 when adding
	root   string
	typing bool // the folder is being edited in the footer
	res    slot[[]found]
	picked map[string]bool // by path
	cur    int
	off    int
}

// scanFolders finds the git repositories under root and names each after its GitHub repository, or its folder.
// shortcut: reads each origin one after another, run them in parallel if large folders feel slow.
func scanFolders(ctx context.Context, src gitx.Source, root string, demo bool) ([]found, error) {
	var paths []string
	if demo {
		for _, f := range gitx.DemoFolders {
			paths = append(paths, root+"/"+f)
		}
	} else {
		var err error
		if paths, err = config.Scan(ctx, root, scanDepth); err != nil {
			return nil, err
		}
	}
	out := make([]found, 0, len(paths))
	for _, p := range paths {
		name := filepath.Base(p)
		if slug, err := src.Slug(ctx, p); err == nil {
			name = slug[strings.LastIndex(slug, "/")+1:]
		}
		out = append(out, found{p, name})
	}
	return out, nil
}

// canEdit reports whether the repository list may be changed: never over a file that failed to load.
func (m *Model) canEdit() bool {
	return m.opts.Demo || m.opts.ConfigErr == nil || errors.Is(m.opts.ConfigErr, config.ErrNotFound)
}

// canAdd offers a in Connections, and anywhere while no repository is configured.
func (m *Model) canAdd() bool {
	return m.canEdit() && (m.view == viewConnections || len(m.repos) == 0)
}

// connRepo is the repository of the selected Connections row, or -1.
func (m *Model) connRepo() int {
	if m.view != viewConnections || m.focus == paneSidebar {
		return -1
	}
	if i := m.cursor[viewConnections] - repoRows(m.connections()); i >= 0 && i < len(m.repos) {
		return i
	}
	return -1
}

func (m *Model) canManage() bool { return m.canEdit() && !m.inspect && m.connRepo() >= 0 }

func (m *Model) canFix() bool {
	i := m.connRepo()
	return m.canEdit() && i >= 0 && m.repos[i].cfg.Status != config.StatusOK
}

func (m *Model) repoConfigs() []config.Repo {
	out := make([]config.Repo, len(m.repos))
	for i, r := range m.repos {
		out[i] = r.cfg
	}
	return out
}

// applyRepos saves a changed repository list, then shows it. A failed save leaves the file and the screen as
// they were. A folder still in the list keeps its state object and anything loading into it, so a reorder
// reloads nothing; a renamed one reloads its git data, which carries the name, and keeps its GitHub data.
func (m *Model) applyRepos(next []config.Repo, done string) tea.Cmd {
	if !m.opts.Demo {
		if m.changedOnDisk() {
			m.flash = "Nothing saved: " + clean(filepath.Base(m.opts.ConfigPath)) + " changed outside git-tui. Restart it to load the changes."
			return nil
		}
		if err := config.Save(m.opts.ConfigPath, config.Config{Repositories: next}); err != nil {
			m.flash = "Could not save the configuration: " + clean(err.Error())
			return nil
		}
		m.opts.ConfigErr = nil // a missing file exists now
	}
	sel := ""
	if r := m.selected(); r != nil {
		sel = r.cfg.Path
	}
	old := m.repos
	m.repos = nil
	used := map[*repoState]bool{} // a hand-written config can list one folder twice; each entry keeps its own state
	for _, c := range next {
		i := slices.IndexFunc(old, func(r *repoState) bool { return !used[r] && r.cfg == c })
		if i < 0 { // renamed
			i = slices.IndexFunc(old, func(r *repoState) bool { return !used[r] && r.cfg.Path == c.Path })
		}
		switch {
		case i < 0:
			m.repos = append(m.repos, newRepo(c))
		default:
			r := old[i]
			used[r] = true
			if r.cfg.Name != c.Name { // a running load into these slots no longer matches their generation
				r.summary, r.log, r.tags = slot[gitx.Summary]{}, slot[[]gitx.Commit]{}, slot[[]gitx.Tag]{}
			}
			r.cfg = c
			m.repos = append(m.repos, r)
		}
	}
	m.closeDiff()
	m.resetCompare(viewOverview) // no view owns it
	m.repo = slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Path == sel }) + 1
	if !slices.Contains(m.groups(), m.group) { // its last repository left it
		m.group = ""
	}
	m.setGroup(m.group)
	m.flash = done
	return m.ensure(false)
}

// changedOnDisk reports whether the file no longer holds the list on screen, as when someone edited it by hand;
// saving over it would lose that edit.
func (m *Model) changedOnDisk() bool {
	disk, err := config.Load(m.opts.ConfigPath)
	if errors.Is(err, config.ErrNotFound) {
		return len(m.repos) > 0
	}
	return err != nil || !slices.EqualFunc(disk.Repositories, m.repos, func(d config.Repo, r *repoState) bool {
		return d.Name == r.cfg.Name && d.Path == r.cfg.Path && d.Group == r.cfg.Group
	})
}

func (m *Model) moveRepo(d int) tea.Cmd {
	i := m.connRepo()
	j := i + d
	if i < 0 || j < 0 || j >= len(m.repos) {
		return nil
	}
	next := m.repoConfigs()
	next[i], next[j] = next[j], next[i]
	cmd := m.applyRepos(next, "")
	if m.repos[j].cfg.Path == next[j].Path { // saved: the cursor follows the repository
		m.cursor[viewConnections] += d
	}
	return cmd
}

func (m *Model) askRemove() {
	i := m.connRepo()
	if i < 0 {
		return
	}
	name := m.repos[i].cfg.Name
	m.confirm = "Remove " + clean(name) + " from the configuration? The folder is not touched."
	m.onYes = func() tea.Cmd {
		return m.applyRepos(slices.Delete(m.repoConfigs(), i, i+1), "Removed "+clean(name)+".")
	}
}

func (m *Model) startRename() tea.Cmd {
	i := m.connRepo()
	if i < 0 {
		return nil
	}
	m.renaming = true
	m.input.Prompt = "Name: "
	m.input.SetValue(m.repos[i].cfg.Name)
	m.input.CursorEnd()
	return m.input.Focus()
}

// rename gives the selected repository a new name; names must stay unique, since views and PRs key on them.
func (m *Model) rename(name string) tea.Cmd {
	i := m.connRepo()
	name = strings.TrimSpace(name)
	switch {
	case i < 0 || name == m.repos[i].cfg.Name:
		return nil
	case name == "":
		m.flash = "A repository needs a name."
		return nil
	case slices.ContainsFunc(m.repos, func(r *repoState) bool { return r.cfg.Name == name }):
		m.flash = "Another repository is already called " + clean(name) + "."
		return nil
	}
	next := m.repoConfigs()
	next[i].Name = name
	return m.applyRepos(next, "Renamed to "+clean(name)+".")
}

// scanRoot is where adding looks first: the folder last used, else the folder the repositories share, else ~/Code.
func (m *Model) scanRoot() string {
	if m.opts.StatePath != "" {
		if st, _ := config.LoadState(m.opts.StatePath); st.ScanRoot != "" {
			return st.ScanRoot
		}
	}
	var root string
	for i, r := range m.repos {
		dir := filepath.Dir(r.cfg.Path)
		if i == 0 {
			root = dir
			continue
		}
		for root != "." && root != "/" && dir != root && !strings.HasPrefix(dir, root+"/") {
			root = filepath.Dir(root)
		}
	}
	if root == "" || root == "." || root == "/" {
		return "~/Code"
	}
	return config.Tilde(root)
}

// openScan opens the add screen with the folder ready to edit; fix >= 0 finds that repository's folder again.
func (m *Model) openScan(fix int) tea.Cmd {
	m.scan = scanState{open: true, fix: fix, root: m.scanRoot(), picked: map[string]bool{}, res: slot[[]found]{gen: m.scan.res.gen + 1}}
	return m.editRoot()
}

func (m *Model) editRoot() tea.Cmd {
	m.scan.typing = true
	m.input.Prompt = "Look in: "
	m.input.SetValue(m.scan.root)
	m.input.CursorEnd()
	return m.input.Focus()
}

func (m *Model) closeScan() {
	if m.scan.res.cancel != nil {
		m.scan.res.cancel()
	}
	m.scan = scanState{res: slot[[]found]{gen: m.scan.res.gen + 1}} // a late result is dropped
}

func (m *Model) runScan() tea.Cmd {
	root := strings.TrimSpace(m.input.Value())
	if root == "" {
		m.flash = "Type a folder to look in."
		return nil
	}
	src, demo := m.opts.Source, m.opts.Demo
	if abs, err := filepath.Abs(root); err == nil && !demo && root != "~" && !strings.HasPrefix(root, "~/") {
		root = config.Tilde(abs) // remembered and shown absolute, so it means the same folder from anywhere
	}
	m.scan.typing = false
	m.input.Blur()
	m.scan.root = root
	m.scan.cur, m.scan.off, m.scan.res.loaded, m.scan.res.val = 0, 0, false, nil
	m.savePrefs(func(s *config.State) { s.ScanRoot = root })
	return start(m, &m.scan.res, true, kindScan, -1, func(ctx context.Context) ([]found, error) {
		return scanFolders(ctx, src, root, demo)
	})
}

// scanned puts the cursor on the likeliest folder when finding a moved repository.
func (m *Model) scanned() {
	if m.scan.fix < 0 || m.scan.fix >= len(m.repos) || !m.scan.res.loaded {
		return
	}
	r := m.repos[m.scan.fix].cfg
	i := slices.IndexFunc(m.scan.res.val, func(f found) bool {
		return filepath.Base(f.path) == filepath.Base(r.Path) || f.name == r.Name
	})
	if i >= 0 {
		m.scan.cur = i
		m.keepScanVisible()
		m.flash = "Found " + clean(config.Tilde(m.scan.res.val[i].path)) + ". Enter points " + clean(r.Name) + " there."
	} else {
		m.flash = "No folder called " + clean(filepath.Base(r.Path)) + " found. Choose one, or press tab to look elsewhere."
	}
}

// configured reports whether a folder is already in the list.
func (m *Model) configured(path string) bool {
	return slices.ContainsFunc(m.repos, func(r *repoState) bool { return r.cfg.Path == path })
}

func (m *Model) scanKey(s string) tea.Cmd {
	rows := m.scan.res.val
	switch s {
	case "esc":
		m.closeScan()
	case "tab":
		return m.editRoot()
	case "j", "down":
		m.scan.cur = min(m.scan.cur+1, max(0, len(rows)-1))
		m.keepScanVisible()
	case "k", "up":
		m.scan.cur = max(m.scan.cur-1, 0)
		m.keepScanVisible()
	case "space":
		if m.scan.fix < 0 && m.scan.cur < len(rows) && !m.configured(rows[m.scan.cur].path) {
			p := rows[m.scan.cur].path
			m.scan.picked[p] = !m.scan.picked[p]
		}
	case "enter":
		if m.scan.fix >= 0 {
			return m.fixPath()
		}
		return m.addPicked()
	}
	return nil
}

func (m *Model) keepScanVisible() {
	h := max(1, m.layout().bodyH-3)
	m.scan.off = min(max(m.scan.off, m.scan.cur-h+1), m.scan.cur)
}

// addPicked adds the chosen folders. A name already in use gets its parent folder in front: client-x/api.
func (m *Model) addPicked() tea.Cmd {
	next := m.repoConfigs()
	taken := func(n string) bool { return slices.ContainsFunc(next, func(r config.Repo) bool { return r.Name == n }) }
	added := 0
	for _, f := range m.scan.res.val {
		if !m.scan.picked[f.path] {
			continue
		}
		name := f.name
		if taken(name) {
			name = filepath.Base(filepath.Dir(f.path)) + "/" + name
		}
		if taken(name) {
			name = config.Tilde(f.path) // paths are unique, so this always is
		}
		r := config.Repo{Name: name, Path: f.path, Status: config.StatusOK}
		if !m.opts.Demo {
			r = config.Validate(r)
		}
		next = append(next, r)
		added++
	}
	if added == 0 {
		m.flash = "Press space to choose the repositories to add."
		return nil
	}
	m.closeScan()
	noun := " repositories."
	if added == 1 {
		noun = " repository."
	}
	return m.applyRepos(next, "Added "+strconv.Itoa(added)+noun)
}

// fixPath points the repository being fixed at the folder under the cursor.
func (m *Model) fixPath() tea.Cmd {
	rows, i := m.scan.res.val, m.scan.fix
	if m.scan.cur >= len(rows) || i >= len(m.repos) {
		return nil
	}
	p := rows[m.scan.cur].path
	if m.configured(p) {
		m.flash = "That folder is already configured."
		return nil
	}
	next := m.repoConfigs()
	next[i].Path, next[i].Status, next[i].Err = p, config.StatusOK, ""
	if !m.opts.Demo {
		next[i] = config.Validate(next[i])
	}
	m.closeScan()
	return m.applyRepos(next, clean(next[i].Name)+" now points at "+clean(config.Tilde(p))+".")
}

// scanLines is the add screen: the folder searched, then each repository found in it.
func (m *Model) scanLines(h int) []string {
	title := " Add repositories"
	if m.scan.fix >= 0 && m.scan.fix < len(m.repos) {
		title = " Find the folder for " + clean(m.repos[m.scan.fix].cfg.Name)
	}
	lines := []string{m.st.titleFocus.Render(title), m.st.muted.Render(" In " + clean(m.scan.root) + ", " + strconv.Itoa(scanDepth) + " folders deep"), ""}
	res := &m.scan.res
	switch {
	case m.scan.typing && !res.loaded && !res.loading:
		return append(lines, m.st.muted.Render(" Type the folder to look in, then press Enter."))
	case res.loading:
		return append(lines, m.st.muted.Render(" Looking for git repositories…"))
	case res.err != nil:
		return append(lines, m.st.bad.Render(" Could not look there: "+clean(res.err.Error())))
	case len(res.val) == 0:
		return append(lines, m.st.muted.Render(" No git repositories here. Press tab to look elsewhere."))
	}
	end := min(len(res.val), m.scan.off+h-len(lines))
	for i := m.scan.off; i < end; i++ {
		f := res.val[i]
		box, st := "[ ] ", m.st.text
		switch {
		case m.configured(f.path):
			box, st = "[·] ", m.st.muted
		case m.scan.picked[f.path]:
			box, st = "[x] ", m.st.ok
		}
		if m.scan.fix >= 0 {
			box = ""
		}
		segs := []seg{{box + pad(clean(f.name), 24), st}, {clean(config.Tilde(f.path)), m.st.muted}}
		lines = append(lines, m.row(segs, m.width, i == m.scan.cur && !m.scan.typing, false)...)
	}
	return lines
}
