package chat

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"slices"
	"strings"
)

// The demo's attachments and custom emoji are drawn here, in memory: no
// network, no files.

var fakeFiles = map[string]File{
	"F1": {ID: "F1", Name: "runners-dashboard.png", Mime: "image/png", W: 640, H: 360},
	"F2": {ID: "F2", Name: "architecture.png", Mime: "image/png", W: 600, H: 400},
	"F3": {ID: "F3", Name: "release-dashboard.png", Mime: "image/png", W: 640, H: 360},
	"F4": {ID: "F4", Name: "service-map.png", Mime: "image/png", W: 600, H: 400},
}

var fakeEmoji = []string{"sltty", "shipit", "party-parrot"}

func (f *Fake) CustomEmoji(context.Context) ([]string, error) {
	return slices.Clone(fakeEmoji), nil
}

func (f *Fake) File(_ context.Context, id string) ([]byte, error) {
	var im image.Image
	if full, ok := strings.CutPrefix(id, "full:"); ok && fakeFiles[full].ID != "" {
		id = full // the same picture: it is drawn at its size
	}
	switch {
	case id == "F1":
		im = dashboard()
	case id == "F2" || id == "F4":
		im = diagram()
	case id == "F3":
		im = dashboard()
	case strings.HasPrefix(id, "emoji:") && slices.Contains(fakeEmoji, id[6:]):
		im = customEmoji(id[6:])
	case strings.HasPrefix(id, "avatar:") && len(id) > 7:
		im = avatar(id[7:])
	default:
		return nil, fmt.Errorf("no file %q", id)
	}
	var b bytes.Buffer
	if err := png.Encode(&b, im); err != nil {
		return nil, err
	}
	return b.Bytes(), nil
}

func fill(im *image.RGBA, r image.Rectangle, c color.RGBA) {
	r = r.Intersect(im.Bounds())
	for y := r.Min.Y; y < r.Max.Y; y++ {
		for x := r.Min.X; x < r.Max.X; x++ {
			im.SetRGBA(x, y, c)
		}
	}
}

func rgb(h uint32) color.RGBA { return color.RGBA{uint8(h >> 16), uint8(h >> 8), uint8(h), 255} }

// dashboard is a bar chart with a line over it, Grafana style.
func dashboard() image.Image {
	im := image.NewRGBA(image.Rect(0, 0, 640, 360))
	fill(im, im.Bounds(), rgb(0x181b1f))
	fill(im, image.Rect(0, 0, 640, 36), rgb(0x22252b))
	fill(im, image.Rect(16, 14, 200, 22), rgb(0xd8d9da))
	for y := 80; y < 340; y += 52 {
		fill(im, image.Rect(40, y, 620, y+1), rgb(0x2c3235))
	}
	for i := range 24 {
		h := 40 + int(120*math.Abs(math.Sin(float64(i)/3))) + i*3
		x := 44 + i*24
		c := rgb(0x73bf69)
		if h > 170 {
			c = rgb(0xf2cc0c)
		}
		if h > 200 {
			c = rgb(0xf2495c)
		}
		fill(im, image.Rect(x, 340-h, x+16, 340), c)
	}
	prev := 0
	for x := 44; x < 620; x++ {
		y := 150 + int(40*math.Sin(float64(x)/50))
		if prev != 0 {
			fill(im, image.Rect(x, min(y, prev), x+1, max(y, prev)+3), rgb(0x5794f2))
		}
		prev = y
	}
	return im
}

// diagram is three boxes and the arrows between them.
func diagram() image.Image {
	im := image.NewRGBA(image.Rect(0, 0, 600, 400))
	fill(im, im.Bounds(), rgb(0xf6f8fa))
	boxes := []image.Rectangle{image.Rect(40, 60, 200, 150), image.Rect(400, 60, 560, 150), image.Rect(220, 260, 380, 350)}
	colors := []uint32{0x0366d6, 0x28a745, 0x6f42c1}
	for i, b := range boxes {
		fill(im, b, rgb(colors[i]))
		fill(im, b.Inset(6), rgb(0xffffff))
		fill(im, image.Rect(b.Min.X+20, b.Min.Y+38, b.Max.X-20, b.Min.Y+50), rgb(colors[i]))
	}
	fill(im, image.Rect(200, 103, 400, 107), rgb(0x586069))
	fill(im, image.Rect(118, 150, 122, 305), rgb(0x586069))
	fill(im, image.Rect(118, 303, 220, 307), rgb(0x586069))
	fill(im, image.Rect(478, 150, 482, 305), rgb(0x586069))
	fill(im, image.Rect(380, 303, 482, 307), rgb(0x586069))
	return im
}

// customEmoji draws a custom emoji, 64 × 64.
func customEmoji(name string) image.Image {
	im := image.NewRGBA(image.Rect(0, 0, 64, 64))
	disc := func(cx, cy, r float64, c color.RGBA) {
		for y := range 64 {
			for x := range 64 {
				if math.Hypot(float64(x)-cx, float64(y)-cy) <= r {
					im.SetRGBA(x, y, c)
				}
			}
		}
	}
	switch name {
	case "sltty": // a blue face
		disc(32, 32, 30, rgb(0x1d9bd1))
		disc(22, 26, 5, rgb(0xffffff))
		disc(42, 26, 5, rgb(0xffffff))
		fill(im, image.Rect(20, 42, 44, 47), rgb(0xffffff))
	case "shipit": // a squirrel-ish brown blob with a hat
		disc(32, 38, 24, rgb(0x8b5a2b))
		fill(im, image.Rect(18, 6, 46, 16), rgb(0x222222))
		fill(im, image.Rect(12, 16, 52, 20), rgb(0x222222))
		disc(24, 34, 4, rgb(0xffffff))
		disc(40, 34, 4, rgb(0xffffff))
	case "party-parrot": // rainbow rings
		for i, c := range []uint32{0xe40303, 0xff8c00, 0xffed00, 0x008026, 0x004dff, 0x750787} {
			disc(32, 32, float64(30-i*5), rgb(c))
		}
	}
	return im
}

// avatar is a person's picture: a head and shoulders on a color of their
// own.
func avatar(who string) image.Image {
	h := uint32(0)
	for _, r := range who {
		h = h*31 + uint32(r)
	}
	bgs := []uint32{0x2eb67d, 0xe01e5a, 0x36c5f0, 0xecb22e, 0x611f69, 0x1264a3}
	im := image.NewRGBA(image.Rect(0, 0, 64, 64))
	fill(im, im.Bounds(), rgb(bgs[h%uint32(len(bgs))]))
	skin := rgb(0xf1c27d)
	for y := range 64 {
		for x := range 64 {
			head := math.Hypot(float64(x)-32, float64(y)-26) <= 13
			body := math.Hypot(float64(x)-32, float64(y)-66) <= 26
			if head || body {
				im.SetRGBA(x, y, skin)
			}
		}
	}
	return im
}
