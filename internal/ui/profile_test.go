package ui

import (
	"context"
	"errors"
	"fmt"
	"image"
	"strings"
	"testing"
	"time"

	tea "charm.land/bubbletea/v2"
	"github.com/charmbracelet/colorprofile"
	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"

	"github.com/shaungsimpson/git-tui/internal/gitx"
)

func TestProfileHeader(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	m := setup(t, o, 120, 30)
	out := ansi.Strip(m.render())
	for _, want := range []string{"Alice", "@alice", "Acme · Remote", "review request", "128 followers"} {
		if !strings.Contains(out, want) {
			t.Fatalf("full header lacks %q:\n%s", want, out)
		}
	}
	lines := strings.Split(m.render(), "\n")
	if len(lines) != 30 || !strings.Contains(ansi.Strip(lines[bannerRows]), "Overview") {
		t.Fatalf("the tabs should sit under the banner in a 30-line frame (%d lines)", len(lines))
	}

	press(m, "b") // ASCII
	for i, l := range strings.Split(ansi.Strip(m.render()), "\n")[:bannerRows] {
		for _, r := range l {
			if r > 0x7e {
				t.Fatalf("ASCII header line %d has %q: %s", i, r, l)
			}
		}
	}
	press(m, "b") // text
	if m.profileRows() != 1 || !strings.Contains(ansi.Strip(m.render()), "@alice | ") {
		t.Fatal("text mode is one line")
	}
	press(m, "b") // off
	if m.profileRows() != 0 || strings.Contains(ansi.Strip(m.render()), "@alice") {
		t.Fatal("off hides the header")
	}
}

func TestProfileGivesWayOnShortWindows(t *testing.T) {
	o := demoOpts()
	o.Profile = ProfileFull
	m := setup(t, o, 60, 16)
	if m.profileShown() != ProfileText {
		t.Fatalf("a 16-line window shows %v, want the one-line header", profileNames[m.profileShown()])
	}
	press(m, "g", "p")
	if f := ansi.Strip(m.footer()); !strings.HasSuffix(strings.TrimSpace(f), "b profile: full") {
		t.Fatalf("the profile toggle stays visible on a narrow footer: %q", f)
	}
	if lines := strings.Split(m.render(), "\n"); len(lines) != 16 {
		t.Fatalf("%d lines", len(lines))
	}
	m.Update(tea.WindowSizeMsg{Width: 60, Height: 14})
	if m.profileShown() != ProfileOff {
		t.Fatal("a window too short for the panes drops the header")
	}
}

func TestProfileStatsCountTheViewer(t *testing.T) {
	m := New(demoOpts())
	now := time.Now()
	m.me.val = gitx.Viewer{Login: "me", GitName: "Me"}
	m.repos[0].gh.prs.val = []gitx.PR{
		{Author: "me", Checks: gitx.ChecksFailing, Review: "CHANGES_REQUESTED"},
		{Author: "other", ForMe: true, URL: "https://github.com/acme/api/pull/1"},
		{Author: "other"},
		{Author: "me", Review: "APPROVED", Checks: gitx.ChecksPassing, URL: "https://github.com/acme/api/pull/2"},
		{Author: "me", Review: "APPROVED", Checks: gitx.ChecksPassing, Draft: true}, // a draft is not ready
	}
	m.repos[3].gh = m.repos[0].gh // a second clone of the same GitHub repository
	m.repos[1].log.val = []gitx.Commit{{Author: "Me", When: now.Add(-time.Hour)}, {Author: "Me", When: now.AddDate(0, 0, -8)}, {Author: "You", When: now}}
	m.repos[2].summary = slot[gitx.Summary]{loaded: true, val: gitx.Summary{Unstaged: 1}}
	got := m.profileStats(now)
	want := profileStats{requested: 1, mine: 3, failing: 1, changes: 1, commits: 1, dirty: 1, ready: 1}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestProfileAvatarFallbacks(t *testing.T) {
	real := fetchAvatar
	img := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fetchAvatar = func(context.Context, gitx.Source, string) (image.Image, error) { return img, nil }
	t.Cleanup(func() { fetchAvatar = real })

	viewer := gitx.Viewer{Login: "me", Name: "Me", AvatarURL: "https://avatars.githubusercontent.com/u/1?v=4"}
	run := func(images bool, colours colorprofile.Profile) (*Model, tea.Cmd) {
		m := New(demoOpts())
		m.opts.Images, m.profile, m.colours = images, ProfileFull, colours
		m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
		m.me = slot[gitx.Viewer]{val: viewer, loaded: true}
		_, cmd := m.Update(m.ensureAvatar()())
		return m, cmd
	}

	m, cmd := run(true, colorprofile.TrueColor)
	if cmd == nil || !strings.Contains(fmt.Sprint(cmd()), "a=p") || !strings.ContainsRune(m.avatarArt(false)[0], kitty.Placeholder) {
		t.Fatal("with Kitty graphics the avatar is a stored picture")
	}
	if free := m.freePics(); free == nil || !strings.Contains(fmt.Sprint(free()), "a=d") {
		t.Fatal("quitting deletes the avatar")
	}
	if m, _ = run(false, colorprofile.TrueColor); !strings.Contains(m.avatarArt(false)[0], "▀") {
		t.Fatal("without graphics, colour terminals get half blocks")
	}
	if m, _ = run(false, colorprofile.ANSI); strings.ContainsAny(ansi.Strip(m.avatarArt(false)[0]), "▀╭") {
		t.Fatal("16-colour terminals get characters")
	}
	if got := m.ensureAvatar(); got != nil {
		t.Fatal("the same avatar is not fetched twice")
	}

	fetched := 0
	fetchAvatar = func(context.Context, gitx.Source, string) (image.Image, error) {
		fetched++
		if fetched == 1 {
			return nil, errors.New("HTTP 500")
		}
		return img, nil
	}
	o := demoOpts()
	o.Profile = ProfileFull
	m = setup(t, o, 120, 30)
	if fetched != 1 || m.avatar.err == nil {
		t.Fatalf("the first avatar download should fail, fetched %d times", fetched)
	}
	press(m, "ctrl+r")
	if fetched != 2 || m.avatar.err != nil || m.avatar.img == nil {
		t.Fatalf("Ctrl+R should retry a failed avatar, fetched %d times, err %v", fetched, m.avatar.err)
	}
	m = New(demoOpts())
	m.me.val = gitx.Viewer{Name: "Mo Bell"} // no picture
	if art := ansi.Strip(strings.Join(m.avatarArt(false), "\n")); !strings.Contains(art, "╭") || !strings.Contains(art, "MB") {
		t.Fatalf("without a picture the avatar is framed initials:\n%s", art)
	}
	if got := sized(viewer.AvatarURL, 64); got != "https://avatars.githubusercontent.com/u/1?s=64&v=4" {
		t.Fatalf("sized: %s", got)
	}
}
