package ui

import (
	"context"
	"slices"
	"time"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/update"
)

// slot holds one asynchronously loaded value. gen identifies the newest request; older results are dropped.
type slot[T any] struct {
	val             T
	err             error
	loaded, loading bool
	gen             int
	cancel          context.CancelFunc
	at, tried       time.Time // the last successful load, and the last attempt
	fails           int       // failures in a row, for the retry backoff
}

// repoState is one configured folder. Its git data is its own; its GitHub data is shared with every other folder
// cloned from the same GitHub repository, so clones load it once and list it once.
type repoState struct {
	cfg     config.Repo
	summary slot[gitx.Summary]
	log     slot[[]gitx.Commit]
	tags    slot[[]gitx.Tag]
	slug    slot[string] // the GitHub "owner/name" of origin, read once
	gh      *ghState
}

// ghState is one GitHub repository's data. Until a folder's slug is known it has a state of its own.
type ghState struct {
	slug string
	prs  slot[[]gitx.PR]
	deps slot[[]gitx.Deployment]
	runs slot[[]gitx.Run]
}

func newRepo(c config.Repo) *repoState { return &repoState{cfg: c, gh: &ghState{}} }

type dataKind int

const (
	kindSummary dataKind = iota
	kindLog
	kindTags
	kindPRs
	kindDeps
	kindRuns
	kindDiff
	kindCompare
	kindViewer
	kindScan
	kindSlug
	kindRelease
)

// github reports whether a kind is GitHub data, shared between clones of one repository.
func (k dataKind) github() bool { return k == kindPRs || k == kindDeps || k == kindRuns }

// resultMsg is a finished load. store puts it in the slot that asked for it, wherever that repository now sits
// in the list; repo is the position when it started, for follow-up work only.
type resultMsg struct {
	kind      dataKind
	repo, gen int
	val       any
	err       error
	store     func(resultMsg)
}

// ensure starts the loads for the data on screen that is stale, or all of it when force is set (Ctrl+R).
func (m *Model) ensure(force bool) tea.Cmd {
	var cmds []tea.Cmd
	type shared struct {
		gh   *ghState
		kind dataKind
	}
	once := map[shared]bool{} // clones share GitHub data, so it loads once even when forced
	for _, w := range m.wanted() {
		if w.kind.github() {
			k := shared{m.repos[w.repo].gh, w.kind}
			if once[k] {
				continue
			}
			once[k] = true
		}
		cmds = append(cmds, m.load(w.repo, w.kind, force))
	}
	if force && m.view == viewConnections && m.opts.Build.Kind != update.Release { // other builds check only when asked
		cmds = append(cmds, m.load(-1, kindRelease, true))
	}
	if m.cmpOpen() && (force || due(&m.cmp.res, localAge)) { // an open commit list is git data on screen too
		cmds = append(cmds, m.loadCompare())
	}
	return tea.Batch(cmds...)
}

func start[T any](m *Model, s *slot[T], force bool, kind dataKind, repo int, fetch func(context.Context) (T, error)) tea.Cmd {
	if !force && !due(s, ageOf(kind)) {
		return nil
	}
	if s.cancel != nil {
		s.cancel()
	}
	m.gens++
	s.gen = m.gens
	ctx, cancel := context.WithCancel(m.ctx)
	s.cancel, s.loading = cancel, true
	gen := s.gen
	return func() tea.Msg {
		defer cancel()
		v, err := fetch(ctx)
		return resultMsg{kind: kind, repo: repo, gen: gen, val: v, err: err, store: func(msg resultMsg) { finish(s, msg) }}
	}
}

// finish stores a result if it answers the newest request. loaded means a value is present;
// a failed reload keeps the previous value visible alongside the error.
func finish[T any](s *slot[T], msg resultMsg) {
	if msg.gen != s.gen {
		return
	}
	s.loading, s.err, s.tried = false, msg.err, clock()
	if msg.err != nil {
		s.fails++
		return
	}
	s.val, s.loaded, s.at, s.fails = msg.val.(T), true, s.tried, 0
}

func (m *Model) load(i int, kind dataKind, force bool) tea.Cmd {
	if kind == kindViewer {
		dir := "" // the first usable repository, so per-directory git identities apply
		if j := slices.IndexFunc(m.repos, func(r *repoState) bool { return r.cfg.Status == config.StatusOK }); j >= 0 {
			dir = m.repos[j].cfg.Path
		}
		src := m.opts.Source
		return start(m, &m.me, force, kind, -1, func(ctx context.Context) (gitx.Viewer, error) { return src.Viewer(ctx, dir) })
	}
	if kind == kindRelease {
		src, path, now := m.opts.Source, m.opts.ReleaseCache, clock()
		return start(m, &m.release, force, kind, -1, func(ctx context.Context) (update.Latest, error) { return update.Check(ctx, src, path, now) })
	}
	r := m.repos[i]
	if r.cfg.Status != config.StatusOK {
		return nil
	}
	src, name, dir := m.opts.Source, r.cfg.Name, r.cfg.Path
	if kind.github() && !r.slug.loaded && (r.slug.err == nil || due(&r.slug, localAge)) {
		// Which GitHub repository this is decides whose data it shares, so GitHub loads wait for it. A failed
		// lookup retries with the usual backoff; meanwhile the load goes ahead, so its own error shows.
		return start(m, &r.slug, false, kindSlug, i, func(ctx context.Context) (string, error) { return src.Slug(ctx, dir) })
	}
	switch kind {
	case kindSummary:
		return start(m, &r.summary, force, kind, i, func(ctx context.Context) (gitx.Summary, error) { return src.Summary(ctx, name, dir) })
	case kindLog:
		return start(m, &r.log, force, kind, i, func(ctx context.Context) ([]gitx.Commit, error) { return src.Log(ctx, name, dir, logDepth) })
	case kindPRs:
		return start(m, &r.gh.prs, force, kind, i, func(ctx context.Context) ([]gitx.PR, error) { return src.PullRequests(ctx, name, dir) })
	case kindDeps:
		return start(m, &r.gh.deps, force, kind, i, func(ctx context.Context) ([]gitx.Deployment, error) { return src.Deployments(ctx, name, dir) })
	case kindRuns:
		return start(m, &r.gh.runs, force, kind, i, func(ctx context.Context) ([]gitx.Run, error) { return src.DeployRuns(ctx, name, dir) })
	default:
		return start(m, &r.tags, force, kind, i, func(ctx context.Context) ([]gitx.Tag, error) { return src.Tags(ctx, name, dir) })
	}
}

// loadCompare always starts a fresh request; any in-flight comparison is cancelled and its result ignored.
func (m *Model) loadCompare() tea.Cmd {
	if m.cmp.repo < 1 || m.cmp.repo > len(m.repos) {
		return nil
	}
	r := m.repos[m.cmp.repo-1]
	src, name, dir, base, head := m.opts.Source, r.cfg.Name, r.cfg.Path, m.cmp.baseRev, m.cmp.headRev
	return start(m, &m.cmp.res, true, kindCompare, m.cmp.repo, func(ctx context.Context) ([]gitx.Commit, error) {
		return src.Compare(ctx, name, dir, base, head)
	})
}

// applyResult stores a load result and keeps the same row selected if the list reorders around it.
func (m *Model) applyResult(msg resultMsg) tea.Cmd {
	var cmd tea.Cmd
	keep := "" // a cursor on the first row stays on top, so newest-first lists show new rows on load
	if keys := m.rowKeys(m.view); m.cursor[m.view] > 0 && m.cursor[m.view] < len(keys) {
		keep = keys[m.cursor[m.view]]
	}
	if msg.store != nil {
		msg.store(msg) // a superseded result is dropped here: its generation no longer matches
	}
	switch msg.kind {
	case kindViewer:
		cmd = m.ensureAvatar()
	case kindScan:
		m.scanned()
	case kindSlug:
		m.shareGitHub()
		cmd = m.ensure(false) // the GitHub loads that waited for it
	case kindPRs:
		cmd = m.watchReviews()
	case kindDeps:
		cmd = m.followDeploy(msg.repo)
	case kindRuns:
		if msg.err == nil { // a failed reload stops polling until Ctrl+R
			cmd = m.schedulePoll()
		}
	}
	if keys := m.rowKeys(m.view); keep != "" {
		if i := slices.Index(keys, keep); i >= 0 {
			m.cursor[m.view] = i
			m.keepVisible(m.view, &m.cursor[m.view], &m.offset[m.view], len(keys))
		}
	}
	m.clampAll()
	return cmd
}

// shareGitHub points each folder whose slug is now known at that GitHub repository's shared data.
func (m *Model) shareGitHub() {
	for _, r := range m.repos {
		if !r.slug.loaded || r.gh.slug != "" {
			continue
		}
		g := m.gh[r.slug.val]
		if g == nil {
			g = &ghState{slug: r.slug.val}
			m.gh[r.slug.val] = g
		}
		r.gh = g
	}
}
