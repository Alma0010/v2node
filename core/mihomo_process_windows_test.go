package core

import (
	"os/exec"
	"syscall"
)

func configureMihomoTestProcess(command *exec.Cmd) {
	command.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: 0x08000000}
}
