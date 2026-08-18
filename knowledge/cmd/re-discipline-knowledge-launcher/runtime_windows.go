//go:build windows

package main

import (
	"errors"
	"fmt"
	"os/exec"
	"unsafe"

	"golang.org/x/sys/windows"
)

func runRuntime(command *exec.Cmd) error {
	job, err := createKillOnCloseJob()
	if err != nil {
		return fmt.Errorf("create runtime job: %w", err)
	}
	defer windows.CloseHandle(job)

	if err := command.Start(); err != nil {
		return err
	}
	if err := bindRuntimeToJob(job, command); err != nil {
		killErr := command.Process.Kill()
		if killErr == nil {
			_ = command.Wait()
		}
		return errors.Join(
			fmt.Errorf("bind runtime process to launcher job: %w", err),
			killErr,
		)
	}
	return command.Wait()
}

func createKillOnCloseJob() (windows.Handle, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return 0, err
	}
	information := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	information.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	if _, err := windows.SetInformationJobObject(
		job,
		windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&information)),
		uint32(unsafe.Sizeof(information)),
	); err != nil {
		windows.CloseHandle(job)
		return 0, err
	}
	return job, nil
}

func bindRuntimeToJob(job windows.Handle, command *exec.Cmd) error {
	process, err := windows.OpenProcess(
		windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false,
		uint32(command.Process.Pid),
	)
	if err != nil {
		return err
	}
	defer windows.CloseHandle(process)
	return windows.AssignProcessToJobObject(job, process)
}
