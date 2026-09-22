// Copyright (c) 2017 Gorillalabs. All rights reserved.

package backend

import (
	"fmt"
	"io"
	"os/exec"
)

type PowerShellVersion int

const (
	Auto              PowerShellVersion = iota
	WindowsPowerShell                   // Windows Powershell (5.1)
	Pwsh                                // pwsh
)

// Local represents a PowerShell session on the local machine, that is, the machine executing this code.
type Local struct {
	// Requested version of PowerShell to start (defaults to Auto)
	Version PowerShellVersion
}

func (l *Local) StartProcess(cmd string, args ...string) (Waiter, io.Writer, io.Reader, io.Reader, error) {
	// #nosec G204: The variables passed to this function are not user-controlled
	command := exec.Command(cmd, args...)

	stdin, err := command.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Could not get hold of the PowerShell's stdin stream: %w", err)
	}

	stdout, err := command.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Could not get hold of the PowerShell's stdout stream: %w", err)
	}

	stderr, err := command.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Could not get hold of the PowerShell's stderr stream: %w", err)
	}

	err = command.Start()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("Could not spawn PowerShell process: %w", err)
	}

	return command, stdin, stdout, stderr, nil
}

func (l *Local) ExecutablePath() (string, error) {
	// If the version is set to Auto, default to whichever version is most
	// sensible for this build. (Windows Powershell for windows binary, pwsh
	// for linux / macOS binary)
	if l.Version == Auto {
		l.Version = DefaultPwsh
	}

	switch l.Version {
	case WindowsPowerShell:
		return "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe", nil
	case Pwsh:
		ps, err := exec.LookPath("pwsh")
		if err != nil {
			return "", fmt.Errorf("could not find pwsh executable")
		}
		return ps, nil
	}

	return "", fmt.Errorf("unsupported PowerShell version value: %d", l.Version)
}
