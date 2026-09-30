package update

import (
	"testing"

	"github.com/libi/libi-print-agent/internal/api"
)

func strPtr(s string) *string { return &s }

func TestShouldUpdate(t *testing.T) {
	cases := []struct {
		name    string
		current string
		info    *api.VersionInfo
		want    bool
		wantErr bool
	}{
		{name: "nil info", current: "1.0.0", info: nil, want: false},
		{name: "no release published", current: "1.0.0", info: &api.VersionInfo{Version: nil}, want: false},
		{name: "newer available", current: "1.0.0", info: &api.VersionInfo{Version: strPtr("1.1.0")}, want: true},
		{name: "same version", current: "1.1.0", info: &api.VersionInfo{Version: strPtr("1.1.0")}, want: false},
		{name: "older than current", current: "1.1.0", info: &api.VersionInfo{Version: strPtr("1.0.0")}, want: false},
		{name: "unparsable current", current: "dev", info: &api.VersionInfo{Version: strPtr("1.0.0")}, wantErr: true},
		{name: "unparsable remote", current: "1.0.0", info: &api.VersionInfo{Version: strPtr("not-a-version")}, wantErr: true},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got, err := ShouldUpdate(c.current, c.info)
			if c.wantErr {
				if err == nil {
					t.Fatalf("ShouldUpdate() expected error, got nil (result %v)", got)
				}
				return
			}
			if err != nil {
				t.Fatalf("ShouldUpdate() unexpected error: %v", err)
			}
			if got != c.want {
				t.Fatalf("ShouldUpdate() = %v, want %v", got, c.want)
			}
		})
	}
}
