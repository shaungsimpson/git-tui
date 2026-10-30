package termimg

import (
	"image"
	"image/color"
	"testing"

	"github.com/charmbracelet/x/ansi"
)

// halves is white on the left, black on the right.
func halves(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := range h {
		for x := range w / 2 {
			img.Set(x, y, color.White)
		}
	}
	return img
}

func TestASCII(t *testing.T) {
	got := ASCII(halves(64, 64), 4, 2, true)
	if len(got) != 2 || got[0] != "@@  " || got[1] != "@@  " {
		t.Fatalf("dark background: %q", got)
	}
	if got := ASCII(halves(64, 64), 4, 1, false); got[0] != "  @@" {
		t.Fatalf("light background inverts: %q", got)
	}
	if got := ASCII(halves(2, 1), 6, 3, true); len(got) != 3 || len(got[0]) != 6 {
		t.Fatalf("an image smaller than the grid still fills it: %q", got)
	}
}

func TestBlocks(t *testing.T) {
	got := Blocks(halves(20, 20), 10, 5)
	if len(got) != 5 {
		t.Fatalf("%d rows", len(got))
	}
	for _, l := range got {
		if w := ansi.StringWidth(l); w != 10 {
			t.Fatalf("row is %d cells wide: %q", w, l)
		}
	}
}

func TestRounded(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	for y := range 64 {
		for x := range 64 {
			src.Set(x, y, color.White)
		}
	}
	red := color.RGBA{R: 255, A: 255}
	out := Rounded(src, 0.25, 4, red)
	if c := out.NRGBAAt(0, 0); c.A != 0 {
		t.Fatalf("corner should be transparent, got %v", c)
	}
	if c := out.NRGBAAt(32, 1); c != (color.NRGBA{R: 255, A: 255}) {
		t.Fatalf("edge should be the border colour, got %v", c)
	}
	if c := out.NRGBAAt(32, 32); c != (color.NRGBA{R: 255, G: 255, B: 255, A: 255}) {
		t.Fatalf("centre should be the picture, got %v", c)
	}
}

func TestRoundedCircle(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 64, 64))
	out := Rounded(src, 0.5, 4, color.White)
	if c := out.NRGBAAt(5, 5); c.A != 0 {
		t.Fatalf("a circle leaves the corner region transparent, got %v", c)
	}
	if c := out.NRGBAAt(32, 1); c.A != 255 {
		t.Fatalf("the top of the circle is drawn, got %v", c)
	}
}
