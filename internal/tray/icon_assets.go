package tray

import (
	"embed"
	"fmt"
)

// icons holds the generated multi-resolution .ico files (see
// tools/genicons and the package doc for how they are produced from the
// LiBi brand logo). This file has no build tag - unlike the rest of the
// Windows-only icon loading code - so parseICO/selectIcoImage get real
// `go test` coverage on every platform this repo builds on, not just
// Windows.
//
//go:embed icons/app.ico icons/connected.ico icons/disconnected.ico icons/unpaired.ico
var icons embed.FS

// icoDataFor returns the embedded .ico bytes for the tray icon's visual
// state. Unknown states fall back to the unpaired icon rather than
// panicking, matching this package's general "degrade, don't crash" stance
// for a background desktop agent.
func icoDataFor(s State) []byte {
	switch s {
	case StateConnected:
		return mustAsset("icons/connected.ico")
	case StateDisconnected:
		return mustAsset("icons/disconnected.ico")
	default:
		return mustAsset("icons/unpaired.ico")
	}
}

// appIconData returns the plain-logo .ico embedded for the (currently
// unused at runtime - see cmd/libi-print-agent's winres.json) app icon.
func appIconData() []byte {
	return mustAsset("icons/app.ico")
}

func mustAsset(path string) []byte {
	data, err := icons.ReadFile(path)
	if err != nil {
		// Embedded at build time; a missing file here is a programming
		// error (a renamed/deleted asset), not a runtime condition.
		panic(fmt.Sprintf("tray: embedded asset %q missing: %v", path, err))
	}
	return data
}

// icoImage is one resolution's worth of icon data parsed out of an .ico
// file's ICONDIR, along with the raw (PNG-compressed, Vista+ format) bytes
// Windows' CreateIconFromResourceEx expects.
type icoImage struct {
	Width, Height int
	Data          []byte
}

// parseICO reads a standard ICONDIR + ICONDIRENTRY[] header (as written by
// tools/genicons) and returns one icoImage per entry, in file order.
func parseICO(data []byte) ([]icoImage, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("ico: file too short (%d bytes)", len(data))
	}
	if data[2] != 1 || data[3] != 0 {
		return nil, fmt.Errorf("ico: not an icon file (type %d)", int(data[2])|int(data[3])<<8)
	}
	count := int(data[4]) | int(data[5])<<8

	const dirEntrySize = 16
	headerEnd := 6 + count*dirEntrySize
	if len(data) < headerEnd {
		return nil, fmt.Errorf("ico: truncated directory (need %d bytes, have %d)", headerEnd, len(data))
	}

	images := make([]icoImage, 0, count)
	for i := 0; i < count; i++ {
		e := data[6+i*dirEntrySize : 6+(i+1)*dirEntrySize]
		width, height := int(e[0]), int(e[1])
		if width == 0 {
			width = 256
		}
		if height == 0 {
			height = 256
		}
		bytesInRes := le32(e[8:12])
		imageOffset := le32(e[12:16])

		start := int(imageOffset)
		end := start + int(bytesInRes)
		if start < 0 || end > len(data) || end < start {
			return nil, fmt.Errorf("ico: entry %d data out of range (offset=%d size=%d file=%d bytes)", i, start, bytesInRes, len(data))
		}
		images = append(images, icoImage{Width: width, Height: height, Data: data[start:end]})
	}
	return images, nil
}

func le32(b []byte) uint32 {
	return uint32(b[0]) | uint32(b[1])<<8 | uint32(b[2])<<16 | uint32(b[3])<<24
}

// selectIcoImage picks the best entry for a desired pixel size: an exact
// match if present, else the smallest entry at least as large as desired
// (so Windows scales a slightly-too-big image down rather than stretching
// a smaller one up and getting blurry), else the single largest available
// entry if desired exceeds every size in images.
//
// images must be non-empty.
func selectIcoImage(images []icoImage, desired int) icoImage {
	best := images[0]
	haveBest := false
	for _, img := range images {
		if img.Width == desired && img.Height == desired {
			return img
		}
		if img.Width >= desired {
			if !haveBest || img.Width < best.Width {
				best = img
				haveBest = true
			}
		}
	}
	if haveBest {
		return best
	}
	// Nothing is big enough: fall back to the single largest entry.
	largest := images[0]
	for _, img := range images {
		if img.Width > largest.Width {
			largest = img
		}
	}
	return largest
}
