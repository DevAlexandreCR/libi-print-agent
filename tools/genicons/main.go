// Command genicons regenerates the tray's embedded .ico assets
// (internal/tray/icons/*.ico) from the LiBi brand logo committed at
// assets/brand/icon.png. Run it with `go run ./tools/genicons` (via Docker,
// like every other Go command in this repo - see README "Building
// locally"). It is a one-off generator, not part of the normal build/test/
// vet pipeline; its output is committed so CI never needs to run it.
//
// Each .ico bundles multiple resolutions (see iconSizes below) as
// PNG-compressed icon entries, which every Windows version this agent
// targets (Vista+) accepts at any size, not only 256x256. app.ico is the
// plain logo (used as the .exe's file icon, see cmd/libi-print-agent's
// winres.json); connected/disconnected/unpaired.ico additionally bake in a
// small status dot bottom-right, so the tray icon's state is visible at a
// glance without reading the tooltip.
package main

import (
	"bytes"
	"encoding/binary"
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"math"
	"os"
	"path/filepath"

	xdraw "golang.org/x/image/draw"
)

const (
	sourcePath = "assets/brand/icon.png"
	outputDir  = "internal/tray/icons"
)

// iconSizes mirrors the common Windows SM_CXSMICON values across the
// standard DPI scaling steps (100%=16, 125%=20, 150%=24, 200%=32, 250%=40,
// 300%=48, 400%=64), plus 256 for the .exe/shell's large-icon views.
var iconSizes = []int{256, 64, 48, 40, 32, 24, 20, 16}

// dotColors maps each tray state to its status-dot color. unpaired has no
// entry: it gets the plain logo, same as app.ico.
var dotColors = map[string]color.NRGBA{
	"connected.ico":    {R: 0x16, G: 0xa3, B: 0x4a, A: 0xff}, // green
	"disconnected.ico": {R: 0xdc, G: 0x26, B: 0x26, A: 0xff}, // red
	"unpaired.ico":     {R: 0x94, G: 0xa3, B: 0xb8, A: 0xff}, // gray
}

// iconFileOrder keeps generation (and log) order deterministic; iterating
// the dotColors map directly would not be.
var iconFileOrder = []string{"connected.ico", "disconnected.ico", "unpaired.ico"}

func main() {
	previewDir := flag.String("preview-dir", "", "optional: also write one plain PNG per (icon, size) here for visual inspection")
	logoOut := flag.String("logo-png", "", "optional: also write a single plain-logo PNG (see -logo-size) here - used to feed the status page's inline favicon/header logo, and winres.json's exe icon source")
	logoSize := flag.Int("logo-size", 64, "size in pixels for -logo-png")
	flag.Parse()

	if err := run(*previewDir, *logoOut, *logoSize); err != nil {
		fmt.Fprintln(os.Stderr, "genicons:", err)
		os.Exit(1)
	}
}

func run(previewDir, logoOut string, logoSize int) error {
	src, err := loadSource(sourcePath)
	if err != nil {
		return fmt.Errorf("load %s: %w", sourcePath, err)
	}

	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		return err
	}

	if err := writeIconSet(filepath.Join(outputDir, "app.ico"), src, nil, previewDir, "app"); err != nil {
		return fmt.Errorf("app.ico: %w", err)
	}
	for _, name := range iconFileOrder {
		dot := dotColors[name]
		label := name[:len(name)-len(".ico")]
		if err := writeIconSet(filepath.Join(outputDir, name), src, &dot, previewDir, label); err != nil {
			return fmt.Errorf("%s: %w", name, err)
		}
	}

	if logoOut != "" {
		rgba := resize(src, logoSize)
		if err := writePNGFile(logoOut, rgba); err != nil {
			return fmt.Errorf("logo png: %w", err)
		}
		fmt.Println("wrote", logoOut)
	}

	fmt.Printf("wrote %s/{app,connected,disconnected,unpaired}.ico\n", outputDir)
	return nil
}

func loadSource(path string) (image.Image, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return png.Decode(f)
}

// writeIconSet resizes src to every size in iconSizes, optionally stamping a
// status dot, and writes the resulting multi-resolution .ico to outPath. If
// previewDir is non-empty, it also writes each individual size as a loose
// PNG there, named "<label>_<size>.png", for visual inspection.
func writeIconSet(outPath string, src image.Image, dot *color.NRGBA, previewDir, label string) error {
	var entries []icoEntry
	for _, size := range iconSizes {
		rgba := resize(src, size)
		if dot != nil {
			drawStatusDot(rgba, *dot)
		}

		if previewDir != "" {
			if err := writePNGFile(filepath.Join(previewDir, fmt.Sprintf("%s_%d.png", label, size)), rgba); err != nil {
				return err
			}
		}

		var buf bytes.Buffer
		if err := png.Encode(&buf, rgba); err != nil {
			return err
		}
		entries = append(entries, icoEntry{width: size, height: size, data: buf.Bytes()})
	}

	f, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer f.Close()
	return writeICO(f, entries)
}

func writePNGFile(path string, img image.Image) error {
	if dir := filepath.Dir(path); dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return err
		}
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return png.Encode(f, img)
}

// resize downscales (or upscales) src to size x size using a high-quality
// Catmull-Rom filter, which - unlike the standard library's image/draw,
// which only supports same-scale compositing - correctly handles the large
// reduction ratios here (512 -> 16 is a 32x minification).
func resize(src image.Image, size int) *image.NRGBA {
	dst := image.NewNRGBA(image.Rect(0, 0, size, size))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// drawStatusDot stamps a small status-colored circle in the bottom-right
// corner of img, in place. The dot is wrapped in a white ring, itself
// wrapped in a thin dark ring, so it keeps contrast whether the taskbar
// (light or dark) ends up showing the white or the dark edge against its
// own background.
//
// Sizing is proportional to the icon (not a fixed pixel count), tuned by
// eye against the 16 and 32px renders: a dot at ~34% of the icon's diameter
// reads clearly as a status indicator without swallowing the logo glyph
// (40% was tried first and judged too dominant at 16px).
func drawStatusDot(img *image.NRGBA, dotColor color.NRGBA) {
	size := float64(img.Bounds().Dx())

	colorRadius := size * 0.17
	whiteRing := math.Max(1, size*0.05)
	darkRing := math.Max(0.75, size*0.035)
	outerRadius := colorRadius + whiteRing + darkRing

	// Centered so the dark ring's outer edge just touches the icon's
	// bottom-right corner.
	cx := size - outerRadius
	cy := size - outerRadius

	fillCircleAA(img, cx, cy, outerRadius, color.NRGBA{R: 0x0a, G: 0x0a, B: 0x0a, A: 0xb0})
	fillCircleAA(img, cx, cy, colorRadius+whiteRing, color.NRGBA{R: 0xff, G: 0xff, B: 0xff, A: 0xff})
	fillCircleAA(img, cx, cy, colorRadius, dotColor)
}

// fillCircleAA alpha-composites ("over") a filled circle onto img, with a
// one-pixel-wide antialiased edge so small sizes (16px) stay crisp instead
// of showing a jagged boundary.
func fillCircleAA(img *image.NRGBA, cx, cy, radius float64, c color.NRGBA) {
	bounds := img.Bounds()
	minX := clampInt(int(math.Floor(cx-radius-1)), bounds.Min.X, bounds.Max.X)
	maxX := clampInt(int(math.Ceil(cx+radius+1)), bounds.Min.X, bounds.Max.X)
	minY := clampInt(int(math.Floor(cy-radius-1)), bounds.Min.Y, bounds.Max.Y)
	maxY := clampInt(int(math.Ceil(cy+radius+1)), bounds.Min.Y, bounds.Max.Y)

	for y := minY; y < maxY; y++ {
		for x := minX; x < maxX; x++ {
			dx := float64(x) + 0.5 - cx
			dy := float64(y) + 0.5 - cy
			dist := math.Sqrt(dx*dx + dy*dy)
			coverage := radius + 0.5 - dist
			if coverage <= 0 {
				continue
			}
			if coverage > 1 {
				coverage = 1
			}
			blendOver(img, x, y, c, coverage)
		}
	}
}

func clampInt(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// blendOver composites straight-alpha color c (scaled by coverage, for
// antialiasing) over the pixel at (x, y) using the standard "over" operator.
// img is image.NRGBA, so its stored bytes are already straight (not
// premultiplied) alpha and can be blended directly without a premultiply/
// unpremultiply round trip.
func blendOver(img *image.NRGBA, x, y int, c color.NRGBA, coverage float64) {
	srcA := float64(c.A) / 255 * coverage
	if srcA <= 0 {
		return
	}
	i := img.PixOffset(x, y)
	dr := float64(img.Pix[i+0])
	dg := float64(img.Pix[i+1])
	db := float64(img.Pix[i+2])
	da := float64(img.Pix[i+3]) / 255

	outA := srcA + da*(1-srcA)
	var outR, outG, outB float64
	if outA > 0 {
		outR = (float64(c.R)*srcA + dr*da*(1-srcA)) / outA
		outG = (float64(c.G)*srcA + dg*da*(1-srcA)) / outA
		outB = (float64(c.B)*srcA + db*da*(1-srcA)) / outA
	}
	img.Pix[i+0] = clampByte(outR)
	img.Pix[i+1] = clampByte(outG)
	img.Pix[i+2] = clampByte(outB)
	img.Pix[i+3] = clampByte(outA * 255)
}

func clampByte(v float64) byte {
	if v < 0 {
		return 0
	}
	if v > 255 {
		return 255
	}
	return byte(v + 0.5)
}

// icoEntry is one resolution's worth of PNG-compressed icon data, ready to
// be written into an .ico's directory + payload.
type icoEntry struct {
	width, height int
	data          []byte
}

// writeICO writes a standard ICONDIR + ICONDIRENTRY[] header followed by
// each entry's raw PNG bytes - the modern (Vista+) PNG-compressed icon
// format, accepted by Windows at any of the declared sizes, not only 256.
func writeICO(w io.Writer, entries []icoEntry) error {
	var out bytes.Buffer

	// ICONDIR: reserved(2)=0, type(2)=1 (icon), count(2).
	binary.Write(&out, binary.LittleEndian, uint16(0))
	binary.Write(&out, binary.LittleEndian, uint16(1))
	binary.Write(&out, binary.LittleEndian, uint16(len(entries)))

	offset := uint32(6 + 16*len(entries))
	for _, e := range entries {
		w8, h8 := byte(e.width), byte(e.height)
		if e.width >= 256 {
			w8 = 0
		}
		if e.height >= 256 {
			h8 = 0
		}
		out.WriteByte(w8)
		out.WriteByte(h8)
		out.WriteByte(0)                                    // color count: n/a for 32bpp
		out.WriteByte(0)                                    // reserved
		binary.Write(&out, binary.LittleEndian, uint16(1))  // color planes
		binary.Write(&out, binary.LittleEndian, uint16(32)) // bits per pixel
		binary.Write(&out, binary.LittleEndian, uint32(len(e.data)))
		binary.Write(&out, binary.LittleEndian, offset)
		offset += uint32(len(e.data))
	}

	for _, e := range entries {
		out.Write(e.data)
	}

	_, err := w.Write(out.Bytes())
	return err
}
