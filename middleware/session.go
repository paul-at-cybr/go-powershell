// Copyright (c) 2017 Gorillalabs. All rights reserved.

package middleware

import (
	"fmt"
	"strings"

	"github.com/paul-at-cybr/go-powershell/utils"
)

type session struct {
	upstream Middleware
	name     string
}

// NewSession starts a PowerShell session (i.e. New-PSSession)
func NewSession(upstream Middleware, config *SessionConfig) (Middleware, error) {
	asserted, ok := config.Credential.(credential)
	if ok {
		credentialParamValue, err := asserted.prepare(upstream)
		if err != nil {
			return nil, fmt.Errorf("could not setup credentials: %w", err)
		}

		config.Credential = credentialParamValue
	}

	name := "goSess" + utils.CreateRandomString(8)
	args := strings.Join(config.ToArgs(), " ")

	_, _, err := upstream.Execute(fmt.Sprintf("$%s = New-PSSession %s", name, args))
	if err != nil {
		return nil, fmt.Errorf("could not create new PSSession: %w", err)
	}

	return &session{upstream, name}, nil
}

// Execute executes PowerShell script in the current session
func (s *session) Execute(cmd string) (string, string, error) {
	return s.upstream.Execute(fmt.Sprintf("Invoke-Command -Session $%s -Script {%s}", s.name, cmd))
}

// Exit disconnects the session (i.e. Disconnect-PSSession)
func (s *session) Exit() {
	// Discard errors here. We are binning the session
	_, _, _ = s.upstream.Execute(fmt.Sprintf("Disconnect-PSSession -Session $%s", s.name))
	s.upstream.Exit()
}
