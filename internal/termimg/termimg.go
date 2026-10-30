// Package termimg fetches pictures from GitHub and draws them with the Kitty graphics protocol's Unicode
// placeholders, which a cell-based renderer such as Bubble Tea treats as ordinary text.
package termimg

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	_ "image/gif" // decoders for the formats GitHub screenshots use
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"time"

	"github.com/charmbracelet/x/ansi"
	"github.com/charmbracelet/x/ansi/kitty"
)

const (
	maxBytes  = 10 << 20
	maxPixels = 16_000_000
	maxRows   = 40
)

// Supported reports whether the terminal draws Kitty Unicode placeholders. tmux is left out (it would need
// passthrough), and so are terminals not yet tested with placeholders, such as WezTerm.
func Supported(getenv func(string) string) bool {
	if getenv("TMUX") != "" {
		return false
	}
	return getenv("TERM_PROGRAM") == "ghostty" || getenv("KITTY_WINDOW_ID") != "" || getenv("TERM") == "xterm-kitty"
}

// Link is a picture referenced from Markdown or HTML.
type Link struct{ Alt, URL string }

var linkRE = regexp.MustCompile(`!\[([^\]]*)\]\(\s*<?([^)\s>]+)>?[^)]*\)|<img\b[^>]*\bsrc\s*=\s*["']([^"']+)["'][^>]*>`)
var altRE = regexp.MustCompile(`\balt\s*=\s*["']([^"']*)["']`)

// Links finds the pictures in one line of a Markdown body.
func Links(line string) []Link {
	var links []Link
	for _, m := range linkRE.FindAllStringSubmatch(line, -1) {
		if m[2] != "" {
			links = append(links, Link{Alt: m[1], URL: m[2]})
			continue
		}
		alt := ""
		if a := altRE.FindStringSubmatch(m[0]); a != nil {
			alt = a[1]
		}
		links = append(links, Link{Alt: alt, URL: m[3]})
	}
	return links
}

// Allowed reports whether raw is a GitHub-hosted picture. No other host is ever contacted, so a PR body
// cannot make the app fetch an arbitrary or internal address.
func Allowed(raw string) bool {
	u, err := url.Parse(raw)
	if err != nil || u.Scheme != "https" || u.User != nil {
		return false
	}
	switch u.Host {
	case "github.com":
		return strings.HasPrefix(u.Path, "/user-attachments/assets/")
	case "user-images.githubusercontent.com", "private-user-images.githubusercontent.com", "raw.githubusercontent.com":
		return true
	}
	return false
}

// githubHost is the only host a login token is sent to; a variable so tests can use a local server.
var githubHost = "github.com"

// client follows GitHub's redirects to signed storage links, but only over https.
var client = &http.Client{
	Timeout: 20 * time.Second,
	CheckRedirect: func(req *http.Request, via []*http.Request) error {
		if req.URL.Scheme != "https" || len(via) >= 5 {
			return errors.New("redirect refused")
		}
		return nil
	},
}

// Download fetches a GitHub-hosted picture of at most maxBytes. token, if set, is sent only to github.com:
// private repositories' attachments need it. The client drops it when GitHub redirects to its storage host.
func Download(ctx context.Context, raw, token string) ([]byte, error) {
	if !Allowed(raw) {
		return nil, errors.New("not a GitHub image link")
	}
	return download(ctx, raw, token)
}

func download(ctx context.Context, raw, token string) ([]byte, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, raw, nil)
	if err != nil {
		return nil, err
	}
	if token != "" && req.URL.Hostname() == githubHost {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxBytes {
		return nil, errors.New("image larger than 10 MB")
	}
	return data, nil
}

// Picture is an image the terminal can store once and draw at any width: Transmit sends it,
// Place sizes it, and Lines draws it.
type Picture struct {
	ID, Width, Height int // Width and Height in pixels
	Transmit          string
}

// Encode decodes PNG, JPEG or GIF data into a picture with the given terminal image ID.
func Encode(data []byte, id int) (Picture, error) {
	cfg, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		return Picture{}, errors.New("not a PNG, JPEG or GIF image")
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || cfg.Width*cfg.Height > maxPixels {
		return Picture{}, errors.New("image dimensions too large")
	}
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return Picture{}, err
	}
	var b bytes.Buffer
	err = kitty.EncodeGraphics(&b, img, &kitty.Options{
		Action: kitty.Transmit, Format: kitty.PNG, Transmission: kitty.Direct, ID: id, Quiet: 2, Chunk: true,
	})
	if err != nil {
		return Picture{}, err
	}
	return Picture{ID: id, Width: cfg.Width, Height: cfg.Height, Transmit: b.String()}, nil
}

// Rows is how many cell rows the picture takes when drawn cols cells wide (cells are about twice as tall as wide).
func (p Picture) Rows(cols int) int {
	return min(maxRows, max(1, (cols*p.Height+p.Width)/(2*p.Width)))
}

// Place sizes the stored picture for cols cells. A fixed placement ID makes a new size replace the old one.
func (p Picture) Place(cols int) string {
	return ansi.KittyGraphics(nil, "a=p", "U=1", fmt.Sprintf("i=%d", p.ID), "p=1",
		fmt.Sprintf("c=%d", cols), fmt.Sprintf("r=%d", p.Rows(cols)), "q=2")
}

// Lines are the placeholder rows that draw the picture cols wide; their foreground colour carries the image ID.
func (p Picture) Lines(cols int) []string {
	style := ansi.Style{}.ForegroundColor(color.RGBA{R: byte(p.ID >> 16), G: byte(p.ID >> 8), B: byte(p.ID), A: 255})
	lines := make([]string, p.Rows(cols))
	for row := range lines {
		var b strings.Builder
		for col := range cols {
			b.WriteRune(kitty.Placeholder)
			b.WriteRune(kitty.Diacritic(row))
			b.WriteRune(kitty.Diacritic(col))
		}
		lines[row] = style.Styled(b.String())
	}
	return lines
}

// Delete frees a picture's memory in the terminal.
func Delete(id int) string {
	return ansi.KittyGraphics(nil, "a=d", "d=I", fmt.Sprintf("i=%d", id), "q=2")
}
