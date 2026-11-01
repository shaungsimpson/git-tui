package ui

import (
	"context"
	"strings"

	tea "charm.land/bubbletea/v2"

	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

// picState is one PR-body picture: loading, stored in the terminal, or the reason it is not shown.
// placed is the width it is currently placed at; a resize sends a new placement, never the image again.
type picState struct {
	loading bool
	pic     termimg.Picture
	err     error
	placed  int
}

type picMsg struct {
	url string
	pic termimg.Picture
	err error
}

// picBase keeps this app's terminal image IDs ("gt", for git-tui) apart from other programs' in the same terminal.
const picBase = 0x677400

// fetchPic loads one picture from the source and encodes it; a variable so tests never touch the network.
var fetchPic = func(ctx context.Context, src gitx.Source, url string, id int) (termimg.Picture, error) {
	img, err := src.Image(ctx, url)
	if err != nil {
		return termimg.Picture{}, err
	}
	return termimg.EncodeImage(img, id)
}

// picCols is how wide PR-body pictures are drawn: the details pane, or the list area when details replace it.
func (m *Model) picCols() int {
	l := m.layout()
	w := l.detailW
	if w == 0 {
		w = l.mainW
	}
	return min(80, max(10, w-3))
}

// ensurePics starts loading the selected PR's GitHub pictures, and re-places loaded ones after a width change.
func (m *Model) ensurePics() tea.Cmd {
	if !m.opts.Images || m.view != viewPRs {
		return nil
	}
	n, cur, _ := m.list()
	if n == 0 {
		return nil
	}
	cols := m.picCols()
	shown, _ := bodyLines(m.prList()[*cur].Body)
	var cmds []tea.Cmd
	var place strings.Builder
	for _, line := range shown {
		for _, link := range termimg.Links(line) {
			url := link.URL
			if st, seen := m.pics[url]; seen {
				if !st.loading && st.err == nil && st.placed != cols {
					place.WriteString(st.pic.Place(cols))
					st.placed = cols
				}
				continue
			}
			if !termimg.Allowed(url) {
				continue
			}
			m.pics[url] = &picState{loading: true}
			m.picIDs++
			id, ctx, src := picBase+m.picIDs, m.ctx, m.opts.Source
			cmds = append(cmds, func() tea.Msg {
				pic, err := fetchPic(ctx, src, url, id)
				return picMsg{url, pic, err}
			})
		}
	}
	if place.Len() > 0 {
		cmds = append(cmds, tea.Raw(place.String()))
	}
	return tea.Batch(cmds...)
}

// freePics deletes every picture this app sent, so the terminal does not keep them after exit.
func (m *Model) freePics() tea.Cmd {
	var b strings.Builder
	if m.avatar.pic.ID != 0 {
		b.WriteString(termimg.Delete(m.avatar.pic.ID))
	}
	for _, st := range m.pics {
		if st.pic.ID != 0 {
			b.WriteString(termimg.Delete(st.pic.ID))
		}
	}
	if b.Len() == 0 {
		return nil
	}
	return tea.Raw(b.String())
}
