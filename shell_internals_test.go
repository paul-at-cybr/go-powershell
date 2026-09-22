package powershell

import (
	"context"
	"io"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/paul-at-cybr/go-powershell/utils"
	"github.com/stretchr/testify/require"
)

func TestReadWithContext(t *testing.T) {
	const interCommandDelay = time.Millisecond * 100

	pr, pw := io.Pipe()
	defer func() {
		_ = pr.Close()
		_ = pw.Close()
	}()

	ctx := context.Background()
	buf := make([]byte, 64)

	var wg sync.WaitGroup

	// Simulate the child process writing chunks to stdout.
	chunks := []string{
		"first response\r\n",
		"second response with more data\r\n",
		"final short\n",
	}

	wg.Go(
		func() {
			for _, c := range chunks {
				_, _ = pw.Write([]byte(c))
				// Simulate time gap between commands.
				time.Sleep(interCommandDelay)
			}
			// Stream remains open
		},
	)

	for i, c := range chunks {
		n, err := readWithContext(ctx, pr, buf)
		if err != nil {
			require.ErrorIs(t, err, io.EOF, "unexpected error on first read: %v", err)
		}
		got := string(buf[:n])
		require.Equal(t, c, got, "Chunk #%d: got %q", i+1, got)
	}

	// Now the input is exhausted so read should block
	ctx, cancel := context.WithTimeout(context.Background(), interCommandDelay*2)
	defer cancel()
	n, err := readWithContext(ctx, pr, buf)
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Equal(t, n, 0)
	wg.Wait()
}

func TestStreamReader(t *testing.T) {
	const interCommandDelay = time.Millisecond * 100

	pr, pw := io.Pipe()
	defer func() {
		_ = pr.Close()
		_ = pw.Close()
	}()

	testFunc := func(iter int) {
		ctx, cancel := context.WithTimeout(context.Background(), interCommandDelay*5)
		defer cancel()

		var wg sync.WaitGroup

		// Simulate the child process writing chunks to stdout.
		chunks := []struct {
			data     string
			boundary string
		}{
			{
				data:     "first response\n",
				boundary: createBoundary(),
			},
			{
				data:     "second response with more data that will be more than fits in the sixty four byte buffer that we use for intermediate reads\n",
				boundary: createBoundary(),
			},
			{
				data:     "final short\n",
				boundary: createBoundary(),
			},
		}

		wg.Go(func() {
			for _, c := range chunks {
				data := c.data + c.boundary
				_, err := pw.Write([]byte(data))
				require.NoError(t, err, "error writing stdout simulation pipe")
				// Simulate time gap between commands.
				time.Sleep(interCommandDelay)
			}
			// Stream remains open — no pw.Close() here!
		})

		time.Sleep(time.Millisecond)
		for i, c := range chunks {

			sout := ""
			err := streamReader(ctx, pr, c.boundary, &sout)
			if err != nil {
				require.ErrorIs(t, err, io.EOF, "unexpected error on read #%d, iter #%d: %v", i+1, iter+1, err)
			}
			require.Equal(t, c.data, sout, "Unexpected data on read #%d, iter #%d", i+1, iter+1)
		}

		// Now the pipe is empty, simulate a context cancel
		sout := ""
		err := streamReader(ctx, pr, createBoundary(), &sout)
		require.ErrorIs(t, err, context.DeadlineExceeded, "expected deadline exceeded but got %v", err)
		// We seem to need to send something to un-stall the pipe
		_, _ = pw.Write([]byte(" "))
		wg.Wait()
	}

	// Run the whole test twice to check that having stalled the pipe that it recovers.
	for i := range 2 {
		testFunc(i)
	}
}

func TestDetermineScriptType(t *testing.T) {
	testFile := filepath.Join(os.TempDir(), utils.CreateRandomString(8)+".ps1")

	require.NoError(t, os.WriteFile(testFile, []byte("Write-Host"), 0o600))
	defer func() {
		_ = os.Remove(testFile)
	}()

	tests := []struct {
		name          string
		input         string
		expected      scriptType
		expectedError error
	}{
		{
			name:     "ps1 file exists",
			input:    testFile,
			expected: scriptExternalFile,
		},
		{
			name:          "ps1 file does not exist",
			input:         "asdaer.ps1",
			expected:      scriptExternalFile,
			expectedError: os.ErrNotExist,
		},
		{
			name: "multiline",
			input: `
			Write-Host
			Write-Host`,
			expected: scriptMultiline,
		},
		{
			name:          "command not script",
			input:         `Write-Host "hello"`,
			expected:      scriptUnknown,
			expectedError: ErrScript,
		},
		{
			name:          "command that looks like a filename",
			input:         `Write-Host`,
			expected:      scriptExternalFile,
			expectedError: os.ErrNotExist,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := determineScriptType(test.input)

			if test.expectedError != nil {
				require.ErrorIs(t, err, test.expectedError)
			} else {
				require.NoError(t, err)
			}

			require.Equal(t, test.expected, result)
		})
	}
}
