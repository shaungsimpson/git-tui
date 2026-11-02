package ui

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/shaungsimpson/git-tui/internal/gitx"
	"github.com/shaungsimpson/git-tui/internal/termimg"
)

func TestPullRequestPictures(t *testing.T) {
	var fetched []string
	real := fetchPic
	fetchPic = func(_ context.Context, _ gitx.Source, url string, id int) (termimg.Picture, error) {
		fetched = append(fetched, url)
		return termimg.Picture{ID: id, Width: 400, Height: 200, Transmit: "\x1b_Ga=t,q=2\x1b\\"}, nil
	}
	t.Cleanup(func() { fetchPic = real })

	o := demoOpts()
	o.Images = true
	m := setup(t, o, 140, 40)
	knowSlugs(m) // the PRs below belong to the shared GitHub data
	m.repos[0].gh.prs.val = []gitx.PR{{Repo: "backend", Number: 1, Title: "UI change", URL: "https://github.com/o/r/pull/1",
		Body: "Before:\n![before](https://github.com/user-attachments/assets/aaa)\n<img src=\"https://evil.example/track.png\">"}}
	m.repos[0].gh.prs.loaded, m.repos[0].gh.prs.at = true, clock() // fresh, so the cadence keeps these PRs
	m.selectRepo(1)
	press(m, "g", "p")
	if len(fetched) != 0 {
		t.Fatalf("pictures load only on the detail page, got %v", fetched)
	}
	press(m, "v")
	if len(fetched) != 1 || fetched[0] != "https://github.com/user-attachments/assets/aaa" {
		t.Fatalf("only the GitHub picture should be fetched, got %v", fetched)
	}
	detail := strings.Join(m.detailLines(), "\n")
	if !strings.ContainsRune(detail, kitty.Placeholder) {
		t.Fatal("a loaded picture should be drawn with placeholders")
	}
	if !strings.Contains(detail, "not hosted on GitHub") {
		t.Fatal("a picture on another host should be named but not loaded")
	}
	for i, l := range strings.Split(m.render(), "\n") {
		if w := lipgloss.Width(l); w > 140 {
			t.Fatalf("line %d is %d wide with a picture", i, w)
		}
	}
	press(m, "j", "k") // moving again must not fetch the same picture twice
	if len(fetched) != 1 {
		t.Fatalf("picture fetched again: %v", fetched)
	}
	st := m.pics["https://github.com/user-attachments/assets/aaa"]
	if st.pic.Transmit != "" {
		t.Fatal("the encoded picture should be dropped once it is sent")
	}

	// A resize re-places the stored picture at the new width; it is not downloaded or sent again.
	_, cmd := m.Update(tea.WindowSizeMsg{Width: 70, Height: 40}) // under the 80-column cap, so the width changes
	if len(fetched) != 1 || st.placed != m.picCols() || cmd == nil {
		t.Fatalf("resize: fetched %v, placed %d, want %d", fetched, st.placed, m.picCols())
	}
	if raw, ok := cmd().(tea.RawMsg); !ok || !strings.Contains(fmt.Sprint(raw.Msg), "a=p") || strings.Contains(fmt.Sprint(raw.Msg), "a=t") {
		t.Fatalf("resize should send only a placement, got %#v", cmd())
	}
	if !strings.ContainsRune(strings.Join(m.detailLines(), ""), kitty.Placeholder) {
		t.Fatal("the picture should still draw after a resize")
	}
	if free := m.freePics(); free == nil || !strings.Contains(fmt.Sprint(free()), "a=d") {
		t.Fatal("quitting should delete the pictures from the terminal")
	}
}

func TestPullRequestPictureError(t *testing.T) {
	real := fetchPic
	fetchPic = func(context.Context, gitx.Source, string, int) (termimg.Picture, error) {
		return termimg.Picture{}, errors.New("HTTP 404")
	}
	t.Cleanup(func() { fetchPic = real })

	o := demoOpts()
	o.Images = true
	m := setup(t, o, 140, 40)
	knowSlugs(m) // the PRs below belong to the shared GitHub data
	m.repos[0].gh.prs.val = []gitx.PR{{Repo: "backend", Number: 1, Title: "x",
		Body: `<img alt="shot" src="https://github.com/user-attachments/assets/aaa">`}}
	m.repos[0].gh.prs.loaded, m.repos[0].gh.prs.at = true, clock() // fresh, so the cadence keeps these PRs
	m.selectRepo(1)
	press(m, "g", "p", "v")
	if detail := strings.Join(m.detailLines(), "\n"); !strings.Contains(detail, "[image: shot · not shown: HTTP 404]") {
		t.Fatalf("a failed picture should say why, not keep loading:\n%s", detail)
	}

	const shot = "https://github.com/user-attachments/assets/aaa"
	fetched := 0
	fetchPic = func(_ context.Context, _ gitx.Source, url string, _ int) (termimg.Picture, error) {
		if url == shot { // Ctrl+R also reloads the demo PRs, whose pictures do not count here
			fetched++
		}
		return termimg.Picture{}, errors.New("HTTP 500")
	}
	press(m, "ctrl+r")
	if st := m.pics[shot]; fetched != 1 || st == nil || st.err == nil || st.err.Error() != "HTTP 500" {
		t.Fatalf("Ctrl+R should retry a failed picture, fetched %d times", fetched)
	}
}

// A PR body can link dozens of pictures; only two download and decode at a time, so memory stays bounded.
func TestPicturesLoadTwoAtATime(t *testing.T) {
	var now, most atomic.Int32
	real := fetchPic
	fetchPic = func(context.Context, gitx.Source, string, int) (termimg.Picture, error) {
		n := now.Add(1)
		for m := most.Load(); n > m && !most.CompareAndSwap(m, n); m = most.Load() {
		}
		time.Sleep(20 * time.Millisecond)
		now.Add(-1)
		return termimg.Picture{}, errors.New("stub")
	}
	t.Cleanup(func() { fetchPic = real })

	o := demoOpts()
	o.Images = true
	m := setup(t, o, 140, 40)
	knowSlugs(m)
	var body strings.Builder
	for i := range 8 {
		fmt.Fprintf(&body, "![p](https://github.com/user-attachments/assets/p%d)\n", i)
	}
	m.repos[0].gh.prs.val = []gitx.PR{{Repo: "backend", Number: 1, Title: "x", Body: body.String()}}
	m.repos[0].gh.prs.loaded, m.repos[0].gh.prs.at = true, clock()
	m.selectRepo(1)
	m.view, m.focus, m.inspect = viewPRs, paneDetail, true
	batch, _ := m.ensurePics()().(tea.BatchMsg)
	if len(batch) != 8 {
		t.Fatalf("want 8 picture loads, got %d", len(batch))
	}
	var wg sync.WaitGroup
	for _, cmd := range batch { // as Bubble Tea runs a batch: every command at once
		wg.Go(func() { cmd() })
	}
	wg.Wait()
	if most.Load() != 2 {
		t.Fatalf("pictures loading at once: %d, want 2", most.Load())
	}
}
