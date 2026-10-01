package autostart

import "testing"

func TestStartupApprovedEnabled(t *testing.T) {
	cases := []struct {
		name        string
		data        []byte
		wantEnabled bool
		wantOK      bool
	}{
		{"nil data", nil, false, false},
		{"empty data", []byte{}, false, false},
		{"0x02 enabled", []byte{0x02, 0, 0, 0}, true, true},
		{"0x06 enabled", []byte{0x06, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, true, true},
		{"0x03 disabled", []byte{0x03, 0, 0, 0}, false, true},
		{"0x07 disabled", []byte{0x07, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0}, false, true},
		{"unrecognized byte", []byte{0x01}, false, false},
		{"single byte 0x03", []byte{0x03}, false, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			enabled, ok := startupApprovedEnabled(c.data)
			if enabled != c.wantEnabled || ok != c.wantOK {
				t.Fatalf("startupApprovedEnabled(%v) = (%v, %v), want (%v, %v)", c.data, enabled, ok, c.wantEnabled, c.wantOK)
			}
		})
	}
}
