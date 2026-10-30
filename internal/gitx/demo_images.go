package gitx

import (
	"context"
	"image"
	"image/color"
	"math"
	"strings"
)

// DemoAvatarURL is the demo viewer's picture; Demo.Image draws it instead of downloading anything.
const DemoAvatarURL = "https://avatars.githubusercontent.com/u/0?v=4"

// Image draws fixture pictures: the demo avatar, or a mock screenshot for a PR attachment.
func (Demo) Image(_ context.Context, raw string) (image.Image, error) {
	if strings.HasPrefix(raw, "https://avatars.githubusercontent.com/") {
		return demoAvatar(128), nil
	}
	return demoScreenshot(480, 240), nil
}

// demoAvatar is a light head-and-shoulders silhouette on a teal-to-indigo gradient.
func demoAvatar(size int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	s := float64(size)
	for y := range size {
		for x := range size {
			fx, fy := (float64(x)+0.5)/s, (float64(y)+0.5)/s
			t := (fx + fy) / 2
			c := color.RGBA{R: uint8(40 + 40*t), G: uint8(150 - 70*t), B: uint8(140 + 60*t), A: 255}
			head := math.Hypot(fx-0.5, fy-0.4) < 0.18
			shoulders := math.Pow((fx-0.5)/0.34, 2)+math.Pow((fy-0.98)/0.34, 2) < 1
			if head || shoulders {
				c = color.RGBA{R: 232, G: 240, B: 238, A: 255}
			}
			img.Set(x, y, c)
		}
	}
	return img
}

// demoScreenshot is a mock app screen: a header bar, a card with text lines, and a button.
func demoScreenshot(w, h int) image.Image {
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	fill := func(x0, y0, x1, y1 int, c color.RGBA) {
		for y := y0; y < y1; y++ {
			for x := x0; x < x1; x++ {
				img.Set(x, y, c)
			}
		}
	}
	fill(0, 0, w, h, color.RGBA{244, 246, 245, 255})
	fill(0, 0, w, h/8, color.RGBA{31, 122, 104, 255})
	fill(w/10, h/4, w*9/10, h*7/8, color.RGBA{255, 255, 255, 255})
	for i, frac := range []int{7, 5, 6} {
		y := h/4 + h/10 + i*h/9
		fill(w/10+w/20, y, w/10+w/20+w*frac/12, y+h/24, color.RGBA{200, 204, 208, 255})
	}
	fill(w*6/10, h*7/10, w*8/10, h*7/10+h/10, color.RGBA{95, 179, 161, 255})
	return img
}
