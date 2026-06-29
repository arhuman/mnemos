package version

import (
	"strings"
	"testing"
)

func TestShortReturnsVersion(t *testing.T) {
	if Short() != Version {
		t.Fatalf("Short() = %q, want %q", Short(), Version)
	}
	if Short() == "" {
		t.Fatal("Short() is empty; the Version default should never be blank")
	}
}

func TestInfoIncludesAllFields(t *testing.T) {
	info := Info()
	for _, want := range []string{Version, GitCommit, BuildDate, "go:"} {
		if !strings.Contains(info, want) {
			t.Errorf("Info() = %q, missing %q", info, want)
		}
	}
}
