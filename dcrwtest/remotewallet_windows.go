//go:build windows
// +build windows

// Copyright (c) 2023 The Decred developers
// Use of this source code is governed by an ISC
// license that can be found in the LICENSE file.
package dcrwtest

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setOSWalletCmdOptions sets platform-specific options needed to run dcrwallet.
func setOSWalletCmdOptions(pipeTX, pipeRX *ipcPipePair, cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		AdditionalInheritedHandles: []syscall.Handle{
			syscall.Handle(pipeTX.w.Fd()),
			syscall.Handle(pipeRX.r.Fd()),
		},
	}
}

// appendOSWalletArgs appends platform-specific arguments needed to run dcrwallet.
func appendOSWalletArgs(pipeTX, pipeRX *ipcPipePair, args []string) []string {
	args = append(args, fmt.Sprintf("--pipetx=%d", pipeTX.w.Fd()))
	args = append(args, fmt.Sprintf("--piperx=%d", pipeRX.r.Fd()))
	return args
}
