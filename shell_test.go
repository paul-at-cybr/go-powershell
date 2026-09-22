// Copyright (c) 2025 Firefly IT Consulting Ltd.

package powershell_test

import (
	"context"
	"fmt"
	"math/rand"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/paul-at-cybr/go-powershell"
	"github.com/paul-at-cybr/go-powershell/backend"
	"github.com/paul-at-cybr/go-powershell/utils"
	"github.com/stretchr/testify/require"
)

func TestShell(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	processorCmd := "Get-CimInstance -ClassName Win32_Processor"
	listPathCmd := "gci -Path C:\\"
	if runtime.GOOS != "windows" {
		processorCmd = "Get-Process | Select-Object -First 1"
		listPathCmd = "gci -Path /"
	}

	stdout, _, err := shell.Execute(processorCmd)

	require.NoError(t, err)
	fmt.Println(stdout)
	stdout, _, err = shell.Execute(listPathCmd)
	require.NoError(t, err)
	fmt.Println(stdout)
}

func TestInvalidCommands(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	tests := []struct {
		name        string
		commands    string
		expectError error
	}{
		{
			name:     "Leading line break",
			commands: "\rWrite-Host 'hello'",
		},
		{
			name:     "Trailing line break",
			commands: "Write-Host 'hello'\n",
		},
		{
			name:        "Embedded LF",
			commands:    "Write-Host 'hello'\nWrite-Host 'goodbye'",
			expectError: powershell.ErrInvalidCommandString,
		},
		{
			name:        "Embedded CR",
			commands:    "Write-Host 'hello'\rWrite-Host 'goodbye'",
			expectError: powershell.ErrInvalidCommandString,
		},
		{
			name:        "Embedded CRLF",
			commands:    "Write-Host 'hello'\r\nWrite-Host 'goodbye'",
			expectError: powershell.ErrInvalidCommandString,
		},
		{
			name:     "Two commands separated by ;",
			commands: "Write-Host 'hello' ; Write-Host 'goodbye'",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, err := shell.Execute(test.commands)

			if test.expectError != nil {
				require.ErrorIs(t, err, test.expectError)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

func TestShellWithContext(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	timeout := time.Second

	ctx, cancel := context.WithTimeout(context.Background(), timeout)

	// Sleep past the context deadline
	sleepMSecs := int((timeout + (500 * time.Millisecond)) / time.Millisecond)
	_, _, err = shell.ExecuteWithContext(ctx, fmt.Sprintf("Start-Sleep -Milliseconds %d", sleepMSecs))
	cancel()

	require.Error(t, err)
	require.ErrorIs(t, err, context.DeadlineExceeded)

	// Now make sure the session isn't broken
	ctx, cancel = context.WithTimeout(context.Background(), timeout)
	defer cancel()
	stdout, _, err := shell.ExecuteWithContext(ctx, "Write-Host 'Hello'\r\n")
	require.NoError(t, err, "Second command died")
	fmt.Println(stdout)
}

func TestShellConcurrent(t *testing.T) {
	worker := func(id int, shell powershell.Shell, t *testing.T, wg *sync.WaitGroup) {
		defer wg.Done()
		for i := range 5 {
			sleep := rand.Intn(50) + 50
			cmd := fmt.Sprintf("Start-Sleep -Milliseconds %d; 'Worker %d - Iteration %d'", sleep, id, i)
			stdout, stderr, err := shell.Execute(cmd)
			require.NoError(t, err)
			require.Empty(t, stderr)
			expected := fmt.Sprintf("Worker %d - Iteration %d", id, i)
			require.Equal(t, expected, strings.TrimSpace(stdout))
			fmt.Printf("Worker %d completed iteration %d\n", id, i)
		}
	}

	// choose a backend
	back := &backend.Local{}

	// start a local powershell process
	shell, err := powershell.New(back)
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	// start multiple workers that all use the same shell
	const numWorkers = 5

	var wg sync.WaitGroup
	wg.Add(numWorkers)
	for i := range numWorkers {
		go worker(i, shell, t, &wg)
	}
	wg.Wait()
}

func TestShellExit(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	_ = shell.Exit()

	// call Exit again - should not panic and should report already closed
	require.NotPanics(t, func() {
		require.ErrorIs(t, shell.Exit(), powershell.ErrShellClosed)
	})
}

func TestShellWriteStderr(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	// Any output on stderr will be seen as a command failure!
	_, serr, err := shell.Execute(`[Console]::Error.WriteLine("error")`)
	require.ErrorIs(t, err, powershell.ErrCommandFailed)
	require.Equal(t, "error", strings.TrimSpace(serr))
}

func TestShellExceptionThrown(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	msg := "This is an error"
	_, stderr, err := shell.Execute("throw '" + msg + "'")
	require.ErrorIs(t, err, powershell.ErrCommandFailed)
	require.Equal(t, msg, strings.TrimSpace(stderr))
}

func TestShellWriteClosedShell(t *testing.T) {
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)

	// Close it now
	_ = shell.Exit()

	_, _, err = shell.Execute("Write-Host")
	require.ErrorIs(t, err, powershell.ErrShellClosed)
}

func TestShellExecuteMultilineScript(t *testing.T) {
	script := `
Write-Host "hello"
Write-Host "goodbye"
`
	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	sout, _, err := shell.ExecuteScript(script)
	require.NoError(t, err)
	require.Contains(t, sout, "hello")
	require.Contains(t, sout, "goodbye")
}

func TestShellExecuteScriptFile(t *testing.T) {
	script := `
Write-Host "hello"
Write-Host "goodbye"
`
	scriptFile := filepath.Join(os.TempDir(), utils.CreateRandomString(8)+".ps1")
	require.NoError(t, os.WriteFile(scriptFile, []byte(script), 0o600), "Error writing script file")
	defer func() {
		_ = os.Remove(scriptFile)
	}()

	shell, err := powershell.New(&backend.Local{})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	sout, _, err := shell.ExecuteScript(scriptFile)
	require.NoError(t, err)
	require.Contains(t, sout, "hello")
	require.Contains(t, sout, "goodbye")
}

func TestShellWindowsPowerShell(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows PowerShell test on non-Windows OS")
	}
	shell, err := powershell.New(&backend.Local{Version: backend.WindowsPowerShell})
	require.NoError(t, err)
	defer func() {
		_ = shell.Exit()
	}()

	require.Equal(t, int64(5), shell.Version().Major, "Expected PowerShell major version 5")
}

func TestShellPwsh(t *testing.T) {
	shell, err := powershell.New(&backend.Local{Version: backend.Pwsh})
	defer func() {
		_ = shell.Exit()
	}()
	require.NoError(t, err)
	require.NotNil(t, shell)
	version := shell.Version()
	require.NotNil(t, version)

	v := version.Major
	require.Greater(t, v, int64(5), "Expected PowerShell major version > 5, but got %s. Maybe pwsh is not installed here", v)
}

func TestWithModules(t *testing.T) {
	// These modules ship with both Windows PowerShell and pwsh
	// but are not imported by default
	modules := []string{"Microsoft.PowerShell.Archive", "Microsoft.PowerShell.Security"}

	shell, err := powershell.New(
		&backend.Local{},
		powershell.WithModules(modules...),
	)
	defer func() {
		_ = shell.Exit()
	}()
	require.NoError(t, err)

	for _, mod := range modules {
		out, _, err := shell.Execute(fmt.Sprintf(`if (Get-Module %s) { "YES" } else {"NO"}`, mod))
		require.NoError(t, err)
		require.Equal(t, "YES", strings.TrimSpace(out))
	}
}
