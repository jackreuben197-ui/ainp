package buildinfo

import "testing"

func TestCurrentIncludesGoVersion(t *testing.T) {
	if got := Current(); got.Go == "" {
		t.Fatalf("Current()=%+v, want Go version", got)
	}
}
