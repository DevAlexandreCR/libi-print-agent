package tray

import "testing"

func TestParseICO(t *testing.T) {
	for _, name := range []string{"connected", "disconnected", "unpaired", "app"} {
		data := mustAsset("icons/" + name + ".ico")
		images, err := parseICO(data)
		if err != nil {
			t.Fatalf("parseICO(%s.ico): %v", name, err)
		}

		wantSizes := map[int]bool{16: true, 20: true, 24: true, 32: true, 40: true, 48: true, 64: true, 256: true}
		if len(images) != len(wantSizes) {
			t.Fatalf("parseICO(%s.ico): got %d entries, want %d", name, len(images), len(wantSizes))
		}
		for _, img := range images {
			if img.Width != img.Height {
				t.Errorf("%s.ico: non-square entry %dx%d", name, img.Width, img.Height)
			}
			if !wantSizes[img.Width] {
				t.Errorf("%s.ico: unexpected size %d", name, img.Width)
			}
			delete(wantSizes, img.Width)
			if len(img.Data) == 0 {
				t.Errorf("%s.ico: entry %d has no data", name, img.Width)
			}
			// PNG magic number: every entry should be PNG-compressed (not BMP).
			if len(img.Data) < 8 || img.Data[0] != 0x89 || img.Data[1] != 'P' || img.Data[2] != 'N' || img.Data[3] != 'G' {
				t.Errorf("%s.ico: entry %d is not PNG-compressed", name, img.Width)
			}
		}
		if len(wantSizes) != 0 {
			t.Errorf("%s.ico: missing sizes %v", name, wantSizes)
		}
	}
}

func TestParseICORejectsGarbage(t *testing.T) {
	if _, err := parseICO([]byte{0, 0, 0}); err == nil {
		t.Fatal("expected error for truncated data")
	}
	if _, err := parseICO([]byte{0, 0, 2, 0, 1, 0}); err == nil {
		t.Fatal("expected error for wrong type field")
	}
}

func TestSelectIcoImage(t *testing.T) {
	images := []icoImage{
		{Width: 16, Height: 16, Data: []byte{1}},
		{Width: 32, Height: 32, Data: []byte{2}},
		{Width: 48, Height: 48, Data: []byte{3}},
		{Width: 256, Height: 256, Data: []byte{4}},
	}

	cases := []struct {
		desired  int
		wantSize int
	}{
		{desired: 16, wantSize: 16},   // exact match
		{desired: 28, wantSize: 32},   // smallest that still fits
		{desired: 32, wantSize: 32},   // exact match
		{desired: 50, wantSize: 256},  // next entry (48) is too small, skip to the one that fits
		{desired: 300, wantSize: 256}, // nothing fits: fall back to the largest
	}
	for _, c := range cases {
		got := selectIcoImage(images, c.desired)
		if got.Width != c.wantSize {
			t.Errorf("selectIcoImage(desired=%d) = %d, want %d", c.desired, got.Width, c.wantSize)
		}
	}
}

func TestIcoDataForKnownStates(t *testing.T) {
	for _, s := range []State{StateConnected, StateDisconnected, StateUnpaired} {
		if len(icoDataFor(s)) == 0 {
			t.Errorf("icoDataFor(%v) returned no data", s)
		}
	}
	if len(appIconData()) == 0 {
		t.Error("appIconData() returned no data")
	}
}
