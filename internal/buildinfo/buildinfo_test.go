package buildinfo

import "testing"

func TestVersionResolution(t *testing.T) {
	for _, tt := range []struct{ in, module, want string }{{"v0.2.0", "v0.1.0", "v0.2.0"}, {"dev", "v0.3.0", "v0.3.0"}, {"", "v0.0.0-20260923-abcd", "v0.0.0-20260923-abcd"}, {"dev", "(devel)", "dev"}} {
		if got := resolvedVersion(tt.in, tt.module); got != tt.want {
			t.Fatalf("%+v got %s", tt, got)
		}
	}
}
