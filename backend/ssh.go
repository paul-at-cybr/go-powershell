// Copyright (c) 2017 Gorillalabs. All rights reserved.

package backend

import (
	"fmt"
	"io"
	"regexp"
	"strings"
)

// sshSession exists so we don't create a hard dependency on crypto/ssh.
type sshSession interface {
	Waiter

	StdinPipe() (io.WriteCloser, error)
	StdoutPipe() (io.Reader, error)
	StderrPipe() (io.Reader, error)
	Start(string) error
}

// SSH represents a session on a remote computer, via SSH
type SSH struct {
	Session sshSession
}

func (b *SSH) StartProcess(cmd string, args ...string) (Waiter, io.Writer, io.Reader, io.Reader, error) {
	stdin, err := b.Session.StdinPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("could not get hold of the SSH session's stdin stream: %w", err)
	}

	stdout, err := b.Session.StdoutPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("could not get hold of the SSH session's stdout stream: %w", err)
	}

	stderr, err := b.Session.StderrPipe()
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("could not get hold of the SSH session's stderr stream: %w", err)
	}

	startCmd, err := b.createCmd(cmd, args)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("could not create SSH start command: %w", err)
	}
	err = b.Session.Start(startCmd)
	if err != nil {
		return nil, nil, nil, nil, fmt.Errorf("could not spawn process via SSH: %w", err)
	}

	return b.Session, stdin, stdout, stderr, nil
}

func (b *SSH) createCmd(cmd string, args []string) (string, error) {
	sb := &strings.Builder{}
	_, err := sb.WriteString(cmd + " ")
	if err != nil {
		return "", fmt.Errorf("could not write command to string builder: %w", err)
	}
	simple := regexp.MustCompile(`^[a-z0-9_/.~+-]+$`)

	for _, arg := range args {
		if !simple.MatchString(arg) {
			arg = b.quote(arg)
		}

		_, err := sb.WriteString(arg + " ")
		if err != nil {
			return "", fmt.Errorf("could not write argument to string builder: %w", err)
		}
	}

	return sb.String(), nil
}

func (b *SSH) quote(s string) string {
	return fmt.Sprintf(`%q`, s)
}
