package dcrdtest

import (
	"os/exec"
	"testing"
)

// TestBuilder tests that we can build a new dcrd node.
func TestBuilder(t *testing.T) {
	path, err := buildDcrd()
	if err != nil {
		t.Fatalf("Unable to build dcrd: %v", err)
	}

	t.Logf("Built dcrd at %s", path)

	cmd := exec.Command(path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Unable to fetch dcrd output: %v", err)
	}

	t.Logf("dcrd version: %s", string(output))
}
