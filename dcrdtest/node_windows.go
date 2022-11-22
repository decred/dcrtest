//go:build windows
// +build windows

package dcrdtest

import (
	"fmt"
	"os/exec"
	"syscall"
)

// setOSNodeCmdOptions sets platform-specific options needed to run dcrd.
func setOSNodeCmdOptions(n *nodeConfig, cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{
		AdditionalInheritedHandles: []syscall.Handle{
			syscall.Handle(n.pipeTX.w.Fd()),
		},
	}
}

// appendOSNodeArgs appends platform-specific arguments needed to run dcrd.
func appendOSNodeArgs(n *nodeConfig, args []string) []string {
	args = append(args, fmt.Sprintf("--pipetx=%d", n.pipeTX.w.Fd()))
	return args
}
