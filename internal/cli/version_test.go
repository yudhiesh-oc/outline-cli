package cli

import "testing"

// The version line prefers the release stamp and otherwise reports the module
// version only for tagged installs, so uncommitted builds stay "dev".
func TestResolveVersion(t *testing.T) {
	cases := []struct {
		stamped string
		module  string
		want    string
	}{
		{"0.2.1", "v0.2.0", "0.2.1"},                           // release builds keep their stamp
		{"dev", "v0.2.1", "0.2.1"},                             // go install of a tag
		{"dev", "v0.3.0-rc.1", "0.3.0-rc.1"},                   // tagged prerelease
		{"dev", "v0.2.1-0.20260914013840-b5dd046154c9", "dev"}, // untagged commit
		{"dev", "v0.2.1-0.20260914013840-b5dd046154c9+dirty", "dev"},
		{"dev", "(devel)", "dev"}, // working-tree build
		{"dev", "", "dev"},        // no build info
	}
	for _, c := range cases {
		if got := resolveVersion(c.stamped, c.module); got != c.want {
			t.Errorf("resolveVersion(%q, %q) = %q, want %q", c.stamped, c.module, got, c.want)
		}
	}
}
