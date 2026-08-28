//go:build windows

package server

import (
	"os"
	"os/exec"
)

func terminalCommand(workDir string) *exec.Cmd {
	command := exec.Command("cmd.exe")
	command.Dir = workDir
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	return command
}

func terminateTerminalProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	_ = command.Process.Kill()
	_, _ = command.Process.Wait()
}
