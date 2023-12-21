// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

// TestBuilder tests that we can build a new dcrwallet binary.
func TestBuilder(t *testing.T) {
	path, err := buildDcrw(dcrwMainPkg)
	if err != nil {
		t.Fatalf("Unable to build dcrwallet: %v", err)
	}

	t.Logf("Built dcrwallet at %s", path)

	cmd := exec.Command(path, "--version")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("Unable to fetch dcrwallet output: %v", err)
	}

	t.Logf("dcrwallet version: %s", string(output))

	err = os.RemoveAll(filepath.Dir(path))
	if err != nil {
		t.Fatal(err)
	}
}
