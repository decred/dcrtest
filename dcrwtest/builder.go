// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
)

var (
	// pathToDcrwMtx protects the following fields.
	pathToDcrwMtx sync.RWMutex

	// pathToDcrw points to the test dcrwallet binary. It is supplied
	// through NewWithDCRW or created on the first call to New and used
	// throughout the life of this package.
	pathToDcrw string

	// isBuiltBinary tracks whether the dcrwallet binary pointed to by
	// pathToDcrw was built by an invocation of globalPathToDcw().
	isBuiltBinary bool

	// dcrwMainPkg is the main dcrwallet package version.
	//
	// NOTE: this MUST have the same value as the one required in
	// require.go.
	dcrwMainPkg = "decred.org/dcrwallet/v3"
)

// SetPathToDcrwallet sets the package level dcrwallet executable. All calls to
// New will use the dcrwallet located there throughout their life. If not set
// upon the first call to New, a dcrwallet will be created in a temporary
// directory and pathToDCRW set automatically.
//
// NOTE: This function is safe for concurrent access, but care must be taken
// when setting different paths and using New, as whatever is at pathToDCRW at
// the time will be used to start that instance.
func SetPathToDcrwallet(path string) {
	pathToDcrwMtx.Lock()
	pathToDcrw = path
	isBuiltBinary = false
	pathToDcrwMtx.Unlock()
}

// SetDcrwalletMainPkg sets the version of the main dcrwallet package executable.
// Calls to New that require building a fresh dcrwallet instance will cause
// the specified package to be built.
//
// NOTE: This function is safe for concurrent access, but care must be taken
// when setting different packages and using New, as the value of the package
// set when New() is executed will be used.
func SetDcrwalletMainPkg(pkg string) {
	pathToDcrwMtx.Lock()
	dcrwMainPkg = pkg
	pathToDcrw = ""
	isBuiltBinary = false
	pathToDcrwMtx.Unlock()
}

// buildDcrw builds a dcrwallet binary in a temp file and returns the path to
// the binary. This requires the Go toolchain to be installed and available in
// the machine. The version of the dcrwallet package built depends on the
// currently required version of the decred.org/dcrwallet module, which may be
// defined by either the go.mod file in this package, a main go.mod file (when
// this package is included as a library in a project) or the current
// workspace.
func buildDcrw(dcrwMainPkg string) (string, error) {
	// NOTE: when updating this package, the dummy import in require.go
	// MUST also be updated.
	outDir, err := os.MkdirTemp("", "dcrwtestdcrwallet")
	if err != nil {
		return "", err
	}

	dcrwalletBin := "dcrwallet"
	if runtime.GOOS == "windows" {
		dcrwalletBin += ".exe"
	}

	dcrwPath := filepath.Join(outDir, dcrwalletBin)
	log.Debugf("Building dcrwallet pkg %s in %s", dcrwMainPkg, dcrwPath)
	cmd := exec.Command("go", "build", "-o", dcrwPath, dcrwMainPkg)
	output, err := cmd.CombinedOutput()
	if err != nil {
		log.Error(string(output))
		return "", fmt.Errorf("failed to build dcrwallet: %w", err)
	}

	return dcrwPath, nil
}

// globalPathToDcrw returns the global path to the binary dcrwallet instance.
// If needed, this will attempt to build a test instance of dcrwallet.
func globalPathToDcrw() (string, error) {
	pathToDcrwMtx.Lock()
	defer pathToDcrwMtx.Unlock()
	if pathToDcrw != "" {
		return pathToDcrw, nil
	}

	newPath, err := buildDcrw(dcrwMainPkg)
	if err != nil {
		return "", err
	}
	pathToDcrw = newPath
	isBuiltBinary = true
	return newPath, nil
}

// CleanBuiltDcrwallet cleans the currently used dcrwallet binary dir if it was
// built by this package.
func CleanBuiltDcrwallet() error {
	var err error
	pathToDcrwMtx.Lock()
	if isBuiltBinary && pathToDcrw != "" {
		isBuiltBinary = false
		err = os.RemoveAll(filepath.Dir(pathToDcrw))
		pathToDcrw = ""
	}
	pathToDcrwMtx.Unlock()
	return err

}
