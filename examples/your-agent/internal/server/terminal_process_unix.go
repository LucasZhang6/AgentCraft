//go:build !windows

package server

import (
	"os"
	"os/exec"
	"syscall"
)

func terminalCommand(workDir string) *exec.Cmd {
	shell := os.Getenv("SHELL")
	if shell == "" {
		shell = "/bin/sh"
	}
	command := exec.Command(shell, "-l")
	command.Dir = workDir
	command.Env = append(os.Environ(), "TERM=xterm-256color")
	return command
}

func terminateTerminalProcess(command *exec.Cmd) {
	if command == nil || command.Process == nil {
		return
	}
	if err := syscall.Kill(-command.Process.Pid, syscall.SIGKILL); err != nil {
		_ = command.Process.Kill()
	}
	_, _ = command.Process.Wait()
}
