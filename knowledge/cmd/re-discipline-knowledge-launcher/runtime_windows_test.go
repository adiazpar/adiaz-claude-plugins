//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"testing"
	"time"

	"golang.org/x/sys/windows"
)

func TestClosingRuntimeJobTerminatesChild(t *testing.T) {
	job, err := createKillOnCloseJob()
	if err != nil {
		t.Fatalf("create runtime job: %v", err)
	}

	command := exec.Command(os.Args[0], "-test.run=TestRuntimeJobHelperProcess")
	command.Env = append(os.Environ(), "RE_DISCIPLINE_RUNTIME_JOB_HELPER=1")
	command.Stderr = io.Discard
	stdout, err := command.StdoutPipe()
	if err != nil {
		windows.CloseHandle(job)
		t.Fatalf("open child stdout: %v", err)
	}
	if err := command.Start(); err != nil {
		windows.CloseHandle(job)
		t.Fatalf("start child: %v", err)
	}
	if err := bindRuntimeToJob(job, command); err != nil {
		windows.CloseHandle(job)
		command.Process.Kill()
		command.Wait()
		t.Fatalf("bind child to runtime job: %v", err)
	}
	ready := bufio.NewScanner(stdout)
	if !ready.Scan() || ready.Text() != "ready" {
		windows.CloseHandle(job)
		command.Process.Kill()
		command.Wait()
		t.Fatalf("child did not become ready: %v", ready.Err())
	}
	if err := windows.CloseHandle(job); err != nil {
		command.Process.Kill()
		command.Wait()
		t.Fatalf("close runtime job: %v", err)
	}

	waited := make(chan error, 1)
	go func() { waited <- command.Wait() }()
	select {
	case <-waited:
	case <-time.After(5 * time.Second):
		command.Process.Kill()
		<-waited
		t.Fatal("child survived after the runtime job closed")
	}
}

func TestRuntimeJobHelperProcess(t *testing.T) {
	if os.Getenv("RE_DISCIPLINE_RUNTIME_JOB_HELPER") != "1" {
		return
	}
	fmt.Fprintln(os.Stdout, "ready")
	for {
		time.Sleep(time.Hour)
	}
}
