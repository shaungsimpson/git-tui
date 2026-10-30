package termimg

import (
	"image"
	"image/color"
	"math"
	"strings"

	"github.com/charmbracelet/x/ansi"
)

// Blocks draws img cols cells wide and rows cells tall with upper half blocks, two pixels per cell:
// the top one as the foreground colour, the bottom one as the background. It needs a colour terminal
// but no graphics protocol; the renderer maps the colours down for terminals with fewer of them.
func Blocks(img image.Image, cols, rows int) []string {
	px := sample(img, cols, rows*2)
	lines := make([]string, rows)
	for y := range lines {
		var b strings.Builder
		for x := range cols {
			b.WriteString(ansi.Style{}.ForegroundColor(px[2*y][x]).BackgroundColor(px[2*y+1][x]).Styled("▀"))
		}
		lines[y] = b.String()
	}
	return lines
}

// asciiRamp runs from empty to dense.
const asciiRamp = " .:-=+*#%@"

// ASCII draws img with characters whose density follows brightness, for terminals without colour or Unicode.
// On a dark background bright pixels get dense characters; on a light one, dark pixels do.
func ASCII(img image.Image, cols, rows int, dark bool) []string {
	px := sample(img, cols, rows)
	lines := make([]string, rows)
	for y, row := range px {
		b := make([]byte, cols)
		for x, c := range row {
			lum := (299*int(c.R) + 587*int(c.G) + 114*int(c.B)) / 1000
			if !dark {
				lum = 255 - lum
			}
			b[x] = asciiRamp[lum*(len(asciiRamp)-1)/255]
		}
		lines[y] = string(b)
	}
	return lines
}

// sample averages img into a cols×rows grid. Transparent pixels count as black.
func sample(img image.Image, cols, rows int) [][]color.RGBA {
	bd := img.Bounds()
	span := func(i, n, size, origin int) (int, int) {
		lo, hi := i*size/n, (i+1)*size/n
		return origin + lo, origin + max(hi, lo+1) // an image smaller than the grid repeats pixels
	}
	grid := make([][]color.RGBA, rows)
	for y := range grid {
		grid[y] = make([]color.RGBA, cols)
		y0, y1 := span(y, rows, bd.Dy(), bd.Min.Y)
		for x := range cols {
			x0, x1 := span(x, cols, bd.Dx(), bd.Min.X)
			var r, g, b, n uint64
			for py := y0; py < y1; py++ {
				for px := x0; px < x1; px++ {
					cr, cg, cb, _ := img.At(px, py).RGBA()
					r, g, b, n = r+uint64(cr), g+uint64(cg), b+uint64(cb), n+1
				}
			}
			grid[y][x] = color.RGBA{R: uint8(r / n >> 8), G: uint8(g / n >> 8), B: uint8(b / n >> 8), A: 255}
		}
	}
	return grid
}

// Rounded clips img to a rounded rectangle whose corner radius is radius times the shorter side, and draws
// a border width pixels wide in border just inside the edge. Edges are anti-aliased, and outside the corners
// is transparent, so the terminal's background shows through.
func Rounded(img image.Image, radius, width float64, border color.Color) *image.NRGBA {
	bd := img.Bounds()
	w, h := float64(bd.Dx()), float64(bd.Dy())
	r := radius * min(w, h)
	bc := color.NRGBAModel.Convert(border).(color.NRGBA)
	out := image.NewNRGBA(image.Rect(0, 0, bd.Dx(), bd.Dy()))
	for y := range bd.Dy() {
		for x := range bd.Dx() {
			// Signed distance from the pixel centre to the rounded edge: negative inside.
			qx := math.Abs(float64(x)+0.5-w/2) - (w/2 - r)
			qy := math.Abs(float64(y)+0.5-h/2) - (h/2 - r)
			d := math.Hypot(max(qx, 0), max(qy, 0)) + min(max(qx, qy), 0) - r
			alpha := clamp01(0.5 - d)
			if alpha == 0 {
				continue
			}
			inner := clamp01(-d - width + 0.5) // 1 inside the border, 0 on it
			src := color.NRGBAModel.Convert(img.At(bd.Min.X+x, bd.Min.Y+y)).(color.NRGBA)
			mix := func(a, b uint8) uint8 { return uint8(float64(a)*(1-inner) + float64(b)*inner + 0.5) }
			out.SetNRGBA(x, y, color.NRGBA{
				R: mix(bc.R, src.R), G: mix(bc.G, src.G), B: mix(bc.B, src.B),
				A: uint8(alpha*(float64(bc.A)*(1-inner)+float64(src.A)*inner) + 0.5),
			})
		}
	}
	return out
}

func clamp01(v float64) float64 { return min(max(v, 0), 1) }
