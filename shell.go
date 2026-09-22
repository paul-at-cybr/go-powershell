// Copyright (c) 2017 Gorillalabs. All rights reserved.
// Portions copyright (c) 2025 Firefly IT Consulting Ltd.

// Package powershell provides an interface to run PowerShell code in a
// session that remains "hot", such that you do not have to create a new instance
// of the powershell process with each invocation, with many enhancements over other packages forked from the same source!
// Supports Windows PowerShell and pwsh, contexts and multithreaded use.
package powershell

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"

	"github.com/coreos/go-semver/semver"
	"github.com/fireflycons/go-powershell/backend"
	"github.com/fireflycons/go-powershell/utils"
	"github.com/juju/errors"
	"golang.org/x/sync/errgroup"
)

type scriptType int

const (
	scriptUnknown scriptType = iota
	scriptExternalFile
	scriptMultiline
)

var (
	// ErrInvalidCommandString is returned if the command passed contains any CR or LF characters
	// since this will stall the pipe. Note that leading and trailing space will be trimmed automatically.
	// Multiple commands should be chained with semicolon, not line breaks.
	ErrInvalidCommandString = errors.New("invalid command - probably you should use ExecuteScript")

	// ErrShellClosed is returned if an attempt is made to perform an operation on a closed shell
	ErrShellClosed = errors.New("shell is closed")

	// ErrPipeWrite is returned if there was a problem sending the command
	// to the session's sdtin pipe.
	ErrPipeWrite = errors.New("error sending command")

	// ErrCommandFailed will be returned if any output from the command sent was
	// read from stderr. This will include unhandled exceptions (uncaught throws)
	// and any direct writes to stderr like Console::Error.WriteLine().
	ErrCommandFailed = errors.New("data written to stderr")

	// ErrScript can be returned by the ExecuteScript functions if the
	// type of script passed as an argument cannot be determined
	ErrScript = errors.New("cannot determine script type")

	// ErrModules can be returned when starting a new shell using the [WithModules]
	// argument to initially load modules.
	ErrLoadModules = errors.New("cannot load one or more modules")
)

// ShellOptions represents options passed to the shell when it is started.
type ShellOptions struct {
	modulesToLoad []string
	logger        Logger
}

// ShellOptionFunc describes optional argmuments to add to the [New] call.
type ShellOptionFunc func(*ShellOptions)

// Logger must be implemented to pass your choice of logging backend
// to the shell using [WithLogger] in order to produce a transcript
// of interactions with the shell to a log output.
// See the tests for examples.
type Logger interface {

	// Infof is called to emit an informational message, such as a command to be exectuted
	Infof(format string, v ...any)

	// Errorf is called to emit an error message.
	Errorf(format string, v ...any)
}

// Shell is the interface to a PowerShell session
type Shell interface {
	// Execute runs PowerShell commands in the session instance, capturing stdout and stderr streams
	Execute(cmd string) (string, string, error)

	// ExecuteWithContext runs PowerShell commands in the session instance, capturing stdout and stderr streams.
	//
	// The context allows cancellation of the command. Streams to/from the PowerShell session may
	// block indefinitely if the command is not well formed as the input may still be waiting.
	//
	// Note that if the error is "context deadline exceeded", the underlying session will be
	// unstable and it will be restarted. A restarted shell _may_ be unstable!
	ExecuteWithContext(ctx context.Context, cmd string) (string, string, error)

	// ExecuteScript runs a multiline script or external script file in the session instance, capturing stdout and stderr streams
	//
	// If the argument is a path to an existing script file, then that will be executed,
	// otherwise the value is assumed to be a multiline string.
	ExecuteScript(scriptOrPath string) (string, string, error)

	// ExecuteScriptWithContext runs a multiline script or external script file in the session instance, capturing stdout and stderr streams.
	//
	//
	// If the argument is a path to an existing script file, then that will be executed,
	// otherwise the value is assumed to be a multiline string.
	//
	// The context allows cancellation of the command. Streams to/from the PowerShell session may
	// block indefinitely if the command is not well formed as the input may still be waiting.
	//
	// Note that if the error is "context deadline exceeded", the underlying session will be
	// unstable and it will be restarted. A restarted shell _may_ be unstable!
	ExecuteScriptWithContext(ctx context.Context, scriptOrPath string) (string, string, error)

	// Version returns the PowerShell version as reported by the $Host built-in variable.
	// If there was an error reading this, version.Major will be -1.
	Version() *semver.Version

	// Exit terminates the underlying PowerShell process.
	// Error will be non-nil if shell already closed.
	Exit() error
}

// concrete implementation of shell
type shell struct {
	handle     backend.Waiter
	backend    backend.Starter
	stdin      io.Writer
	stdout     io.Reader
	stderr     io.Reader
	version    *semver.Version
	options    *ShellOptions
	restarting int
	lock       *sync.Mutex
}

// nullLogger is a noop implementation of Logger
// for when WithLogger was not passed
type nullLogger struct {
}

func (nullLogger) Infof(format string, v ...any)  {}
func (nullLogger) Errorf(format string, v ...any) {}

var _ Logger = (*nullLogger)(nil)

// New creates a new PowerShell session
func New(backend backend.Starter, opts ...ShellOptionFunc) (Shell, error) {

	s := &shell{
		backend: backend,
		lock:    &sync.Mutex{},
		options: &ShellOptions{
			logger: &nullLogger{},
		},
	}

	for _, opt := range opts {
		opt(s.options)
	}

	if err := s.start(); err != nil {
		// Still return the shell here
		// as it may have started a process
		// that needs to be cleaned up
		return s, err
	}

	return s, nil
}

// WithModules specifies a list of PowerShell modules to
// import into the shell when it starts
func WithModules(modules ...string) ShellOptionFunc {
	return func(so *ShellOptions) {
		so.modulesToLoad = modules
	}
}

// WithLogger attaches a logger
// to log all commands sent to the PowerShell session
func WithLogger(l Logger) ShellOptionFunc {
	return func(so *ShellOptions) {
		so.logger = l
	}
}

// Execute runs PowerShell commands in the session instance, capturing stdout and stderr streams
//
// This call may block indefinitely if the command is not well formed.
func (s *shell) Execute(cmd string) (string, string, error) {

	return s.ExecuteWithContext(context.TODO(), cmd)
}

// ExecuteWithContext runs PowerShell commands in the session instance, capturing stdout and stderr streams.
//
// The context allows cancellation of the command. Streams to/from the PowerShell session may
// block indefinitely if the command is not well formed as the input may still be waiting.
//
// Note that if the error is "context deadline exceeded", the underlying session will be
// unstable and it will be restarted. A restarted shell _may_ be unstable!
func (s *shell) ExecuteWithContext(ctx context.Context, cmd string) (string, string, error) {

	// Lock the shell so that only one thread can execute a command at a time
	s.lock.Lock()
	sout, serr, err := s.executeWithContext(ctx, cmd)
	s.lock.Unlock()
	return sout, serr, err
}

func (s *shell) Version() *semver.Version {
	return s.version
}

// Exit releases the PowerShell session, terminating the underlying powershell.exe process
func (s *shell) Exit() error {

	// Prevent panics if Exit is called multiple times
	if s == nil || s.handle == nil {
		if s != nil {
			s.options.logger.Errorf("Exit() - %v", ErrShellClosed)
		}
		return ErrShellClosed
	}

	// Discard error here and everywhere else. We are binning the session
	_, _ = s.stdin.Write([]byte("exit" + newline))

	// if it's possible to close stdin, do so (some backends, like the local one,
	// do support it)
	closer, ok := s.stdin.(io.Closer)
	if ok {
		_ = closer.Close()
	}

	_ = s.handle.Wait()

	s.handle = nil
	s.stdin = nil
	s.stdout = nil
	s.stderr = nil
	return nil
}

// ExecuteScript runs a multiline script or external script file in the session instance, capturing stdout and stderr streams
//
// If the argument is a path to an existing script file, then that will be executed,
// otherwise the value is assumed to be a multiline string.
func (s *shell) ExecuteScript(scriptOrPath string) (string, string, error) {

	return s.ExecuteScriptWithContext(context.Background(), scriptOrPath)
}

// ExecuteScriptWithContext runs a multiline script or external script file in the session instance, capturing stdout and stderr streams.
//
// If the argument is a path to an existing script file, then that will be executed,
// otherwise the value is assumed to be a multiline string.
//
// The context allows cancellation of the command. Streams to/from the PowerShell session may
// block indefinitely if the command is not well formed as the input may still be waiting.
//
// Note that if the error is "context deadline exceeded", the underlying session will be
// unstable and it will be restarted. A restarted shell _may_ be unstable!
func (s *shell) ExecuteScriptWithContext(ctx context.Context, scriptOrPath string) (string, string, error) {

	st, err := determineScriptType(scriptOrPath)

	if err != nil {
		s.options.logger.Errorf("Script determination failed with error: %v", err)
		return "", "", err
	}

	// Assume externalFile
	scriptPath := scriptOrPath

	switch st {
	case scriptMultiline:
		path, teardown, err := prepareMultilineScript(scriptOrPath)
		if err != nil {
			s.options.logger.Errorf("Script preparation failed with error: %v", err)
			return "", "", err
		}
		scriptPath = path
		defer teardown()

	case scriptUnknown:
		s.options.logger.Errorf("Script preparation failed with error: %v", ErrScript)
		return "", "", ErrScript
	}

	// Dot source external file
	return s.ExecuteWithContext(ctx, `. "`+scriptPath+`"`)
}

// This is te core method that actually interacts with the PowerShell session
func (s *shell) executeWithContext(ctx context.Context, cmd string) (sout string, serr string, rerr error) {

	sout = ""
	serr = ""
	rerr = nil

	defer func() {
		if rerr != nil {
			s.options.logger.Errorf("Command failed with error: %v", rerr)
		}
	}()

	if s.handle == nil {
		rerr = ErrShellClosed
		return
	}

	// Sanitize command string
	cmd = strings.TrimSpace(cmd)

	if strings.ContainsAny(cmd, "\r\n") {
		// Line breaks within the body of the command string will
		// stall the pipe - nothing will be produced on PowerShell's stdout
		// and everything will hang.
		rerr = ErrInvalidCommandString
		return
	}

	outBoundary := createBoundary()
	errBoundary := createBoundary()

	s.options.logger.Infof("Submitting command: %s", cmd)
	// wrap the command in special markers so we know when to stop reading from the pipes
	// and also a try block to correctly capture errors.
	// The finally block is needed to ensure that the boundaries are always written
	// even if the command itself contains an exit statement.
	full := fmt.Sprintf(
		"try { %s } catch { [Console]::Error.WriteLine($_.Exception.Message) } finally { [Console]::WriteLine('%s'); [Console]::Error.WriteLine('%s') }%s",
		cmd,
		outBoundary,
		errBoundary,
		newline,
	)

	_, err := s.stdin.Write([]byte(full))
	if err != nil {
		return "", "", errors.Wrap(ErrPipeWrite, errors.Annotate(err, cmd))
	}

	// read stdout and stderr
	eg := &errgroup.Group{}

	eg.Go(func() error {
		return streamReader(ctx, s.stdout, outBoundary, &sout)
	})

	eg.Go(func() error {
		return streamReader(ctx, s.stderr, errBoundary, &serr)
	})

	err = eg.Wait()

	if err != nil {
		// DeadlineExceeded or IO errors should be all we get here.
		s.options.logger.Errorf("Command submission failed with %v: %s", err, cmd)
		if errors.Is(err, context.DeadlineExceeded) {

			if s.restarting == 0 {
				s.options.logger.Infof("Restarting session")
				s.restarting++
				if err1 := s.restart(); err1 != nil {
					err = errors.Wrap(err, err1)
				}
			}

			s.restarting = 0
		}

		rerr = err
		return
	}

	if len(serr) > 0 {
		// Any "normal" error, such as an unhandled exception
		// or direct write to stderr will be caught here.
		rerr = errors.Annotate(ErrCommandFailed, cmd)
		return
	}

	rerr = nil
	return
}

func (s *shell) start() error {

	var ps string

	if local, ok := s.backend.(*backend.Local); ok {

		var (
			err error
		)

		switch local.Version {
		case backend.WindowsPowerShell:
			ps = "C:\\Windows\\System32\\WindowsPowerShell\\v1.0\\powershell.exe"
		case backend.Pwsh:
			ps, err = exec.LookPath("pwsh")
			if err != nil {
				// Fallbask to default powershell
				ps = "powershell.exe"
			}
		}
	}

	handle, stdin, stdout, stderr, err := s.backend.StartProcess(ps, "-NoProfile", "-NoExit", "-Command", "-")
	if err != nil {
		s.options.logger.Errorf("Cannot start session: %v", err)
		return err
	}

	s.handle = handle
	s.stdin = stdin
	s.stdout = stdout
	s.stderr = stderr

	ctx, cancel := context.WithTimeout(context.Background(), time.Second*10)
	defer cancel()

	// Read the powershell host's version
	if versionStr, _, err := s.executeWithContext(ctx, `Write-Host "$($host.version.major).$($host.version.minor).$($host.version.build)"`); err == nil {
		if v, err := semver.NewVersion(strings.TrimSpace(versionStr)); err == nil {
			s.version = v
		}
	} else if errors.Is(err, context.DeadlineExceeded) {
		s.options.logger.Errorf("Cannot determine PowerShell version: %v", err)
		return err
	} else {
		s.options.logger.Errorf("Cannot determine PowerShell version: %v", err)
		s.version = &semver.Version{
			Major: -1,
		}
	}

	// Preload any optional modules
	if len(s.options.modulesToLoad) > 0 {
		modules := strings.Join(
			func() []string {
				m := make([]string, 0, len(s.options.modulesToLoad))
				for _, mod := range s.options.modulesToLoad {
					m = append(m, utils.QuoteArg(mod))
				}
				return m
			}(),
			",",
		)
		if _, errStr, err := s.executeWithContext(ctx, modules+" | ForEach-Object { if (Get-Module $_) { Remove-Module $_ } ; Import-Module -Force $_ }"); err != nil {
			err = errors.Wrap(err, ErrLoadModules)
			err = errors.Annotate(err, errStr)
			s.options.logger.Errorf("%v", err)
			return err
		}
	}

	return nil
}

func (s *shell) restart() error {

	s.options.logger.Infof("%s", "Restarting shell")
	_ = s.Exit()
	return s.start()
}

func streamReader(ctx context.Context, stream io.Reader, boundary string, buffer *string) error {

	// read all output until we have found our boundary token
	output := strings.Builder{}
	bufsize := 64
	buf := make([]byte, bufsize)

	var outStr string

	for {
		var (
			read int
			err  error
		)

		if ctx != context.TODO() && ctx != context.Background() {
			read, err = readWithContext(ctx, stream, buf)
		} else {
			read, err = stream.Read(buf)
		}

		if err != nil {
			return err
		}

		output.Write(buf[:read])

		outStr = strings.TrimRight(output.String(), "\r\n")
		//log.Printf("streamReader receive: %s\n", outStr)
		if strings.HasSuffix(outStr, boundary) {
			// Stop reading when boundary is found
			break
		}
	}

	*buffer = strings.TrimSuffix(outStr, boundary)

	return nil
}

func readWithContext(ctx context.Context, r io.Reader, buf []byte) (int, error) {
	pr, pw := io.Pipe()

	// Background chunked copier
	go func() {
		defer func() {
			_ = pw.Close()
		}()

		tmp := make([]byte, len(buf))
		n, err := r.Read(tmp)
		if n > 0 {
			if _, werr := pw.Write(tmp[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			if err != io.EOF {
				_ = pw.CloseWithError(err)
			}
		}
	}()

	// Context canceller
	go func() {
		<-ctx.Done()
		_ = pw.CloseWithError(ctx.Err())
	}()

	total := 0
	for total < len(buf) {
		n, err := pr.Read(buf[total:])
		total += n

		if err != nil {
			if errors.Is(err, io.ErrClosedPipe) {
				return total, ctx.Err()
			}
			return total, err
		}

		// Return early if we got some bytes — short read, stream may remain open
		if n > 0 {
			break
		}

		select {
		case <-ctx.Done():
			return total, ctx.Err()
		default:
			continue
		}
	}

	return total, nil
}

func createBoundary() string {
	return "$boundary" + utils.CreateRandomString(12) + "$"
}

var pathPattern = func() *regexp.Regexp {
	if runtime.GOOS == "windows" {
		return regexp.MustCompile(`^(?:[a-zA-Z]:[\\/](?:[^\\/:*?"<>|\r\n]+[\\/]?)*|\\\\[^\\/:*?"<>|\r\n]+\\[^\\/:*?"<>|\r\n]+(?:\\[^\\/:*?"<>|\r\n]+)*|\.{1,2}(?:[\\/][^\\/:*?"<>|\r\n]+)*|[^\\/:*?"<>|\r\n]+(?:[\\/][^\\/:*?"<>|\r\n]+)*)$`)
	}
	return regexp.MustCompile(`^(?:/(?:[^/\0]+/)*[^/\0]*|\.{1,2}(?:/[^/\0]+)*/?[^/\0]*|[^/\0]+(?:/[^/\0]+)*)$`)
}()

func determineScriptType(scriptOrPath string) (scriptType, error) {

	if strings.ContainsAny(scriptOrPath, "\r\n") {
		// multline text
		return scriptMultiline, nil
	}

	if pathPattern.MatchString(scriptOrPath) {

		s, err := os.Stat(scriptOrPath)
		if err != nil {
			return scriptExternalFile, err
		}
		if s.IsDir() {
			return scriptExternalFile, os.ErrNotExist
		}

		return scriptExternalFile, nil
	}

	return scriptUnknown, ErrScript
}

func prepareMultilineScript(script string) (string, func(), error) {

	path := filepath.Join(os.TempDir(), utils.CreateRandomString(8)+".ps1")

	if err := os.WriteFile(path, []byte(script), 0644); err != nil {
		return "", func() {}, err
	}

	return path,
		func() {
			// teardown
			_ = os.Remove(path)
		},
		nil
}
