package update

import "testing"

func TestParseVersion(t *testing.T) {
	cases := []struct {
		in      string
		want    Version
		wantErr bool
	}{
		{in: "1.2.3", want: Version{Major: 1, Minor: 2, Patch: 3}},
		{in: "v1.2.3", want: Version{Major: 1, Minor: 2, Patch: 3}},
		{in: "1.2.3-beta.1", want: Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "beta.1"}},
		{in: "1.2.3+build5", want: Version{Major: 1, Minor: 2, Patch: 3}},
		{in: "1.2.3-beta+build5", want: Version{Major: 1, Minor: 2, Patch: 3, Prerelease: "beta"}},
		{in: "1.2", wantErr: true},
		{in: "not-a-version", wantErr: true},
		{in: "1.2.-1", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseVersion(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseVersion(%q) expected error, got %+v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseVersion(%q) unexpected error: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseVersion(%q) = %+v, want %+v", c.in, got, c.want)
		}
	}
}

func TestCompare(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"1.0.1", "1.0.0", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.2.0", "1.10.0", -1}, // numeric, not lexical
		{"1.0.0-beta", "1.0.0", -1},
		{"1.0.0", "1.0.0-beta", 1},
		{"1.0.0-alpha", "1.0.0-beta", -1},
	}
	for _, c := range cases {
		a, err := ParseVersion(c.a)
		if err != nil {
			t.Fatalf("ParseVersion(%q) error = %v", c.a, err)
		}
		b, err := ParseVersion(c.b)
		if err != nil {
			t.Fatalf("ParseVersion(%q) error = %v", c.b, err)
		}
		if got := Compare(a, b); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}
