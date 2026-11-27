package update

import "testing"

func TestIsNewer(t *testing.T) {
	tests := []struct {
		candidate string
		current   string
		want      bool
	}{
		{candidate: "1.3.0", current: "1.2.0", want: true},
		{candidate: "v1.3.0", current: "1.2.9", want: true},
		{candidate: "2.0.0", current: "1.99.99", want: true},
		{candidate: "1.2.0", current: "1.2.0", want: false},
		{candidate: "1.2.0", current: "1.3.0", want: false},
		{candidate: "1.2.0-rc.1", current: "1.2.0", want: false},
		{candidate: "1.2.0", current: "1.2.0-rc.1", want: true},
		{candidate: "1.2.0+build.9", current: "1.2.0", want: false},
		{candidate: "1.3.0", current: "dev", want: true},
		{candidate: "1.3.0", current: "", want: true},
		{candidate: "not-a-version", current: "1.2.0", want: false},
	}

	for _, test := range tests {
		if got := IsNewer(test.candidate, test.current); got != test.want {
			t.Fatalf("IsNewer(%q, %q) = %v, want %v", test.candidate, test.current, got, test.want)
		}
	}
}

func TestCompareVersions(t *testing.T) {
	tests := []struct {
		left, right string
		want        int
		ok          bool
	}{
		{left: "1.2.0", right: "1.2.0", want: 0, ok: true},
		{left: "1.2.1", right: "1.2.0", want: 1, ok: true},
		{left: "1.2.0", right: "1.2.1", want: -1, ok: true},
		{left: "1.10.0", right: "1.9.0", want: 1, ok: true},
		{left: "1.2.0-rc.2", right: "1.2.0-rc.10", want: -1, ok: true},
		{left: "1.2.0-rc.2", right: "1.2.0", want: -1, ok: true},
		{left: "dev", right: "1.2.0", want: 0, ok: false},
	}

	for _, test := range tests {
		got, ok := CompareVersions(test.left, test.right)
		if got != test.want || ok != test.ok {
			t.Fatalf("CompareVersions(%q, %q) = (%d, %v), want (%d, %v)", test.left, test.right, got, ok, test.want, test.ok)
		}
	}
}
