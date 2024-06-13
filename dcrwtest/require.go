//go:build require
// +build require

// This file exists to prevent go mod tidy from removing requires on tools.
// It is excluded from the build as it is not permitted to import main packages.

package dcrwtest

import (
	// NOTE: This MUST have the same value as the dcrwMainPkg var defined
	// in builder.go.
	_ "decred.org/dcrwallet/v3"
)
