//go:build !windows

package main

import "os/exec"

func runRuntime(command *exec.Cmd) error {
	return command.Run()
}
