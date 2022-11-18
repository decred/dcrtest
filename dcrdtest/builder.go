package dcrdtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// buildDcrd builds a dcrd binary in a temp file and returns the path to the
// binary. This requires the Go toolchain to be installed and available in
// the machine. The version of the dcrd package built depends on the currently
// required version of the github.com/decred/dcrd module, which may be defined
// by either this go mod, a parent go mod (when this package is included as a
// library in a project) or the current workspace.
func buildDcrd() (string, error) {
	const dcrdMainPkg = "github.com/decred/dcrd"
	outDir, err := os.MkdirTemp("", "dcrdtestdcrdnode")
	if err != nil {
		return "", err
	}

	dcrdBin := "dcrd"
	if runtime.GOOS == "windows" {
		dcrdBin += ".exe"
	}

	dcrdPath := filepath.Join(outDir, dcrdBin)
	cmd := exec.Command("go", "build", "-o", dcrdPath, dcrdMainPkg)
	if err := cmd.Run(); err != nil {
		return "", fmt.Errorf("failed to build dcrd: %v", err)
	}

	return dcrdPath, nil
}
