//go:build ignore

// Command gen-icon renders the "EasySign for Mac" app icon as a 1024x1024 PNG
// with no external assets: a blue rounded-square (macOS-style, with margin for
// the system drop shadow) carrying a bold white checkmark — evoking a verified
// signature / successful login. Edges are anti-aliased by 4x supersampling.
//
// Usage: go run scripts/gen-icon.go <out.png>
package main

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
)

func main() {
	if len(os.Args) < 2 {
		panic("usage: gen-icon <out.png>")
	}
	const (
		size = 1024
		ss   = 4 // supersample factor
		dim  = size * ss
	)

	// Rounded-square geometry (dim space). ~100px margin at 1024 leaves room for
	// the macOS icon drop shadow, matching the platform look.
	margin := 100.0 * ss
	x0, y0 := margin, margin
	x1, y1 := float64(dim)-margin, float64(dim)-margin
	r := 185.0 * ss

	cTop := [3]float64{47, 128, 237} // #2F80ED
	cBot := [3]float64{18, 52, 108}  // #14346C
	white := [3]float64{255, 255, 255}

	// Checkmark polyline (1024 space), scaled into dim space.
	sc := func(x, y float64) [2]float64 { return [2]float64{x * ss, y * ss} }
	a, b, c := sc(350, 545), sc(455, 650), sc(695, 395)
	hw := 42.0 * ss // half stroke width (rounded caps via segment distance)

	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for oy := 0; oy < size; oy++ {
		for ox := 0; ox < size; ox++ {
			var rr, gg, bb, aa float64
			for sy := 0; sy < ss; sy++ {
				for sx := 0; sx < ss; sx++ {
					x := float64(ox*ss+sx) + 0.5
					y := float64(oy*ss+sy) + 0.5
					if !insideRR(x, y, x0, y0, x1, y1, r) {
						continue // transparent outside the rounded square
					}
					col := lerp(cTop, cBot, clamp01(((x-x0)+(y-y0))/((x1-x0)+(y1-y0))))
					if distSeg(x, y, a, b) <= hw || distSeg(x, y, b, c) <= hw {
						col = white
					}
					// color.RGBA is alpha-premultiplied; summing covered samples and
					// dividing by the full sample count yields correct premultiplied
					// values (png.Encode converts back to straight alpha).
					rr += col[0]
					gg += col[1]
					bb += col[2]
					aa += 255
				}
			}
			n := float64(ss * ss)
			img.Set(ox, oy, color.RGBA{u8(rr / n), u8(gg / n), u8(bb / n), u8(aa / n)})
		}
	}

	f, err := os.Create(os.Args[1])
	if err != nil {
		panic(err)
	}
	defer f.Close()
	if err := png.Encode(f, img); err != nil {
		panic(err)
	}
}

// insideRR reports whether (x,y) is inside the rounded rectangle [x0,y0]-[x1,y1]
// with corner radius r, using the clamped-corner-center distance test.
func insideRR(x, y, x0, y0, x1, y1, r float64) bool {
	cx := math.Max(x0+r, math.Min(x, x1-r))
	cy := math.Max(y0+r, math.Min(y, y1-r))
	dx, dy := x-cx, y-cy
	return dx*dx+dy*dy <= r*r
}

// distSeg returns the distance from point (px,py) to segment a-b.
func distSeg(px, py float64, a, b [2]float64) float64 {
	vx, vy := b[0]-a[0], b[1]-a[1]
	t := clamp01(((px-a[0])*vx + (py-a[1])*vy) / (vx*vx + vy*vy))
	return math.Hypot(px-(a[0]+t*vx), py-(a[1]+t*vy))
}

func lerp(c0, c1 [3]float64, t float64) [3]float64 {
	return [3]float64{c0[0] + (c1[0]-c0[0])*t, c0[1] + (c1[1]-c0[1])*t, c0[2] + (c1[2]-c0[2])*t}
}

func clamp01(t float64) float64 {
	if t < 0 {
		return 0
	}
	if t > 1 {
		return 1
	}
	return t
}

func u8(f float64) uint8 {
	if f < 0 {
		return 0
	}
	if f > 255 {
		return 255
	}
	return uint8(f + 0.5)
}
