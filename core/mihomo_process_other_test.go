//go:build !windows

package core

import "os/exec"

func configureMihomoTestProcess(command *exec.Cmd) {}
