package termimg

import (
	"bytes"
	"context"
	"image"
	"image/png"
	"strings"
	"testing"

	"charm.land/lipgloss/v2"
)

func TestLinks(t *testing.T) {
	line := `See ![before](https://github.com/user-attachments/assets/aaa) and <img width="300" alt="after" src="https://github.com/user-attachments/assets/bbb" />`
	got := Links(line)
	if len(got) != 2 || got[0] != (Link{"before", "https://github.com/user-attachments/assets/aaa"}) ||
		got[1] != (Link{"after", "https://github.com/user-attachments/assets/bbb"}) {
		t.Fatalf("%+v", got)
	}
	if len(Links("no pictures [here](https://x)")) != 0 {
		t.Fatal("a plain link is not a picture")
	}
}

func TestAllowed(t *testing.T) {
	for raw, want := range map[string]bool{
		"https://github.com/user-attachments/assets/426fe6f8":       true,
		"https://private-user-images.githubusercontent.com/1/x.png": true,
		"http://github.com/user-attachments/assets/426fe6f8":        false, // not https
		"https://github.com/acme/backend":                           false, // not an attachment
		"https://evil.example/x.png":                                false,
		"https://user@github.com/user-attachments/assets/x":         false,
		"https://169.254.169.254/latest/meta-data":                  false,
	} {
		if Allowed(raw) != want {
			t.Errorf("Allowed(%q) != %v", raw, want)
		}
	}
	if _, err := Download(context.Background(), "https://evil.example/x.png"); err == nil {
		t.Fatal("Download must refuse other hosts before connecting")
	}
}

func TestEncodeAndLines(t *testing.T) {
	var buf bytes.Buffer
	if err := png.Encode(&buf, image.NewRGBA(image.Rect(0, 0, 400, 200))); err != nil {
		t.Fatal(err)
	}
	p, err := Encode(buf.Bytes(), 0x123456)
	if err != nil {
		t.Fatal(err)
	}
	if p.Width != 400 || p.Height != 200 || p.Rows(20) != 5 { // 20 cols × (200/400) ÷ 2 for tall cells
		t.Fatalf("size %dx%d rows %d", p.Width, p.Height, p.Rows(20))
	}
	// Transmit is the protocol's default action, so the stored-only image carries no a= key and no placement.
	if strings.Contains(p.Transmit, "a=") || strings.Contains(p.Transmit, "U=1") || !strings.Contains(p.Transmit, "i=1193046") {
		t.Fatalf("want the image stored once, without a placement: %.80q", p.Transmit)
	}
	place := p.Place(20)
	for _, want := range []string{"a=p", "U=1", "i=1193046", "p=1", "c=20", "r=5"} {
		if !strings.Contains(place, want) {
			t.Fatalf("placement %q missing %s", place, want)
		}
	}
	if len(place) > 100 {
		t.Fatalf("a resize should send a few bytes, not the image: %d", len(place))
	}
	lines := p.Lines(20)
	if len(lines) != 5 || lipgloss.Width(lines[0]) != 20 {
		t.Fatalf("placeholder rows: %d, width %d", len(lines), lipgloss.Width(lines[0]))
	}
	if _, err := Encode([]byte("<svg/>"), 1); err == nil {
		t.Fatal("SVG is not supported and must say so")
	}
}

func TestSupported(t *testing.T) {
	env := func(kv map[string]string) func(string) string { return func(k string) string { return kv[k] } }
	if !Supported(env(map[string]string{"TERM_PROGRAM": "ghostty"})) {
		t.Fatal("Ghostty draws Kitty placeholders")
	}
	if Supported(env(map[string]string{"TERM_PROGRAM": "ghostty", "TMUX": "/tmp/x"})) {
		t.Fatal("inside tmux images are off")
	}
	if Supported(env(map[string]string{"TERM_PROGRAM": "Apple_Terminal"})) {
		t.Fatal("Terminal.app has no Kitty graphics")
	}
}
