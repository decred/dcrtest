//go:build !windows
// +build !windows

// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"os"
	"os/exec"
)

// setOSWalletCmdOptions sets platform-specific options needed to run dcrwallet.
func setOSWalletCmdOptions(pipeTX, pipeRX *ipcPipePair, cmd *exec.Cmd) {
	cmd.ExtraFiles = []*os.File{
		pipeTX.w,
		pipeRX.r,
	}
}

// appendOSWalletArgs appends platform-specific arguments needed to run dcrwallet.
func appendOSWalletArgs(pipeTX, pipeRX *ipcPipePair, args []string) []string {
	args = append(args, "--pipetx=3")
	args = append(args, "--piperx=4")
	return args
}
