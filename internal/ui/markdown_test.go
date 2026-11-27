package ui

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
	"github.com/charmbracelet/x/ansi"

	"github.com/shaungsimpson/git-tui/internal/config"
	"github.com/shaungsimpson/git-tui/internal/gitx"
)

const mdBody = "> [!WARNING]\n> Check \\*this\\* &amp; that.\n\n" +
	"| Package | Change | Age | Type |\n|---|---|---|---|\n" +
	"| [@left-pad/is-even](https://github.com/left-pad/core) | `3.5.43` → `3.5.44` | ![age](https://mend.example/age) | devDependencies |\n\n" +
	"<details>\n<summary>Release notes</summary>\n\n- hidden until opened\n\n</details>\n\n" +
	"- [x] done\n- [ ] to do\n  1. nested\n\n" +
	"<!-- bot state -->\n\n" +
	"Before:\n![before](https://github.com/user-attachments/assets/b)\n\n" +
	"See <a href=\"https://a.example/c\">here</a>, [plain](http://b.example) and **bold**.\n\n" +
	"```\ncode line\n```\n"

func mdText(m *Model, w int, open bool) (string, []string) {
	var text, pics []string
	for _, l := range m.markdown(mdBody, w, open) {
		if l.pic != nil {
			pics = append(pics, l.pic.URL)
		} else {
			text = append(text, l.text)
		}
	}
	return strings.Join(text, "\n"), pics
}

func TestMarkdownFormatsDescriptions(t *testing.T) {
	m := New(Options{})
	out, pics := mdText(m, 100, false)
	plain := ansi.Strip(out)
	for _, want := range []string{"│ Warning", "│ Check *this* & that.", "@left-pad/is-even  3.5.43 → 3.5.44  age  devDependencies",
		"▸ Release notes", "☑ done", "☐ to do", "  1. nested", "Before:", "See here, plain and bold.", "code line"} {
		if !strings.Contains(plain, want) {
			t.Errorf("formatted description missing %q:\n%s", want, plain)
		}
	}
	for _, gone := range []string{"[!WARNING]", "hidden until opened", "bot state", "**", "](", "<a"} {
		if strings.Contains(plain, gone) {
			t.Errorf("formatted description should not show %q:\n%s", gone, plain)
		}
	}
	if got := strings.Join(linkTargets(out), " "); got != "https://github.com/left-pad/core https://a.example/c" {
		t.Errorf("only https links should be linked, got %s", got)
	}
	if !slices.Equal(pics, []string{"https://github.com/user-attachments/assets/b"}) {
		t.Errorf("a picture on its own line should be drawn, a badge in a table not: %v", pics)
	}
	if open, _ := mdText(m, 100, true); !strings.Contains(ansi.Strip(open), "▾ Release notes") || !strings.Contains(ansi.Strip(open), "hidden until opened") {
		t.Errorf("an open section should show its contents:\n%s", ansi.Strip(open))
	}
	narrow, _ := mdText(m, 30, false)
	if !strings.Contains(ansi.Strip(narrow), "Package  @left-pad/is-even") {
		t.Errorf("a table too wide for the pane should stack each row:\n%s", ansi.Strip(narrow))
	}
	for _, w := range []int{30, 60, 100} {
		for _, open := range []bool{false, true} {
			out, _ := mdText(m, w, open)
			for _, l := range strings.Split(out, "\n") {
				if lipgloss.Width(l) > w {
					t.Errorf("at %d columns a line is %d wide: %q", w, lipgloss.Width(l), ansi.Strip(l))
				}
				if o, c := len(linkTargets(l)), strings.Count(l, "\x1b]8;;\a"); o != c {
					t.Errorf("a link should close on the line it opens: %q", l)
				}
			}
		}
	}
}

func TestMarkdownBotAndEdgeHTML(t *testing.T) {
	m := New(Options{})
	draw := func(body string, open bool) (string, string) {
		var b strings.Builder
		for _, l := range m.markdown(body, 80, open) {
			b.WriteString(l.text + "\n")
		}
		return ansi.Strip(b.String()), b.String()
	}
	dependabot := "Bumps x.\n<details>\n<summary>Release notes</summary>\n<p><em>Sourced from <a href=\"https://g.example/x\">x's releases</a>.</em></p>\n<ul>\n<li>Fix a thing</li>\n</ul>\n</details>\n<details>\n<summary>Commits</summary>\n<ul>\n<li>abc Fix</li>\n</ul>\n</details>"
	if plain, _ := draw(dependabot, false); !strings.Contains(plain, "▸ Release notes\n ▸ Commits") || strings.Contains(plain, "Fix a thing") {
		t.Errorf("sections written all in HTML should start closed:\n%s", plain)
	}
	if plain, out := draw(dependabot, true); !strings.Contains(plain, "▾ Release notes\n Sourced from x's releases.") || !strings.Contains(plain, "Fix a thing") ||
		!slices.Contains(linkTargets(out), "https://g.example/x") {
		t.Errorf("open sections should show their contents, links kept:\n%s", plain)
	}
	nested := "<details><summary>A</summary><details><summary>B</summary>inner</details>tail of A</details>after"
	if plain, _ := draw(nested, false); !strings.Contains(plain, "▸ A") || strings.Contains(plain, "tail of A") || strings.Contains(plain, "inner") ||
		!strings.Contains(plain, "after") {
		t.Errorf("a closed section should hide everything up to its own </details>, nested ones included:\n%s", plain)
	}
	for _, body := range []string{
		"<details><summary>Shots</summary>\n\nhidden text </details>\n\nVisible after.",          // closed within a paragraph
		"<details>\n<summary>Shots</summary>\n\n- hidden item\n\n  </details>\n\nVisible after.", // closed within a list item
	} {
		if plain, _ := draw(body, false); !strings.Contains(plain, "▸ Shots") || !strings.Contains(plain, "Visible after.") || strings.Contains(plain, "hidden") {
			t.Errorf("a closed section should end at its </details> wherever it sits:\n%s", plain)
		}
	}
	if plain, _ := draw("> [!NOTE]\n>\n> Separate paragraph.", false); strings.Contains(plain, "[!NOTE]") || !strings.Contains(plain, "│ Note") {
		t.Errorf("an alert marker on its own paragraph should become the label only:\n%s", plain)
	}
	if _, out := draw("Text <a href=\"https://x.example\">open\n\nNext paragraph.", false); len(linkTargets(strings.Split(out, "\n")[2])) != 0 {
		t.Errorf("a link left open should end with its paragraph:\n%q", out)
	}
}

func TestMarkdownKeys(t *testing.T) {
	o := demoOpts()
	o.StatePath = filepath.Join(t.TempDir(), "state.json")
	m := setup(t, o, 120, 40)
	press(m, "g", "p")
	m.cursor[viewPRs] = slices.IndexFunc(m.prList(), func(p gitx.PR) bool { return p.Number == 499 }) // the bot's PR
	press(m, "v")
	page := func() string { return ansi.Strip(strings.Join(m.detailLines(), "\n")) }
	if !strings.Contains(page(), "▸ left-pad/core") || strings.Contains(page(), "Description · raw") {
		t.Fatalf("descriptions should start formatted:\n%s", page())
	}
	press(m, "z")
	if !strings.Contains(page(), "▾ left-pad/core") || !strings.Contains(page(), "keep comments") {
		t.Fatalf("z should open the sections:\n%s", page())
	}
	press(m, "K", "J") // the bot's PR is the last
	if !strings.Contains(page(), "chore(deps)") || strings.Contains(page(), "▾ left-pad/core") {
		t.Fatal("sections should close when another PR is shown")
	}
	press(m, "m")
	if !strings.Contains(page(), "Description · raw") || !strings.Contains(page(), "<details>") {
		t.Fatalf("m should show the markdown source:\n%s", page())
	}
	if st, _ := config.LoadState(o.StatePath); !st.RawMarkdown {
		t.Fatal("the raw choice should be remembered")
	}
	if m.canSections() {
		t.Fatal("z has nothing to open in the raw source")
	}
}
