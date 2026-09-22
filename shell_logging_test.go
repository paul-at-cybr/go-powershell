package powershell_test

import (
	"fmt"
	"log"
	"strings"
	"testing"

	"github.com/paul-at-cybr/go-powershell"
	"github.com/paul-at-cybr/go-powershell/backend"
	"github.com/sirupsen/logrus"
	"github.com/stretchr/testify/require"
)

// an adapter for standard library "log" package
type logAdapter struct {
	logger *log.Logger
}

func (l *logAdapter) Infof(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	l.logger.Printf("INFO:  %s", msg)
}

func (l *logAdapter) Errorf(format string, v ...any) {
	msg := fmt.Sprintf(format, v...)
	l.logger.Printf("ERROR: %s", msg)
}

func TestLogToStandardLibraryLog(t *testing.T) {

	logbuf := &strings.Builder{}

	adapter := &logAdapter{
		logger: log.New(logbuf, "PS: ", 0),
	}

	shell, err := powershell.New(
		&backend.Local{},
		powershell.WithLogger(adapter),
	)
	require.NoError(t, err)

	cmd := `Write-Host 'hello'`
	_, _, err = shell.Execute(cmd)
	require.NoError(t, err)
	require.Contains(t, logbuf.String(), cmd)

	cmd = `throw 'this is an error'`
	_, _, err = shell.Execute(cmd)
	require.ErrorIs(t, err, powershell.ErrCommandFailed)
	require.Contains(t, logbuf.String(), "this is an error")
	require.NoError(t, shell.Exit())

	logbuf.Reset()
	require.Error(t, shell.Exit())
	require.Contains(t, logbuf.String(), powershell.ErrShellClosed.Error())
}

// an adapter for logrus package
type logrusAdapter struct {
	logger *logrus.Logger
}

func (l *logrusAdapter) Infof(format string, v ...any) {
	l.logger.Infof("PS: "+format, v...)
}

func (l *logrusAdapter) Errorf(format string, v ...any) {
	l.logger.Errorf("PS: "+format, v...)
}

func TestLogToLogrus(t *testing.T) {

	logbuf := &strings.Builder{}

	adapter := &logrusAdapter{
		logger: &logrus.Logger{
			Out:       logbuf,
			Formatter: new(logrus.TextFormatter),
			Hooks:     make(logrus.LevelHooks),
			Level:     logrus.DebugLevel,
		},
	}

	shell, err := powershell.New(
		&backend.Local{},
		powershell.WithLogger(adapter),
	)
	require.NoError(t, err)

	cmd := `Write-Host 'hello'`
	_, _, err = shell.Execute(cmd)
	require.NoError(t, err)
	require.Contains(t, logbuf.String(), cmd)

	require.NoError(t, shell.Exit())

	logbuf.Reset()
	require.Error(t, shell.Exit())
	require.Contains(t, logbuf.String(), powershell.ErrShellClosed.Error())
}
