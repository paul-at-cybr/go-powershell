# go-powershell

This is a fork of [a fork](https://github.com/fireflycons/go-powershell) of [the original Gorilla implementation](https://github.com/40a/go-powershell) with many [enhancements](#enhancements) over the other forks found here!

This package was originally inspired by [jPowerShell](https://github.com/profesorfalken/jPowerShell)
and allows one to run and remote-control a PowerShell session. Use this if you
don't have a static script that you want to execute, bur rather run dynamic
commands.

The session is kept hot in a single instance of `powershell.exe` such that you do not have the overhead of creating a new PowerShell process every time you want to run some commands.

## Installation

    go get github.com/paul-at-start/go-powershell

## Usage

To start a PowerShell shell, you need a backend. Backends take care of starting
and controlling the actual `powershell.exe` process. In most cases, you will want
to use the Local backend, which just uses `os/exec` to start the process.

Note that due to how the inter-process communication works with the underlying session, you cannot
include any line breaks in your command string, otherwise it will stall the pipes. The command is sanity-checked
for this before submission and execute methods will fail with [ErrInvalidCommandString].
If you want to send an entire script, this must be placed in the file system and a command sent to dot-source it.

Local session


<details>
<summary>Expand code</summary>

```go
package main

import (
    "context"
	"fmt"

	ps "github.com/paul-at-start/go-powershell"
	"github.com/paul-at-start/go-powershell/backend"
)

func main() {
	// choose a backend, which by default will use Windows Powershell (5.1)
	back := &backend.Local{}

	// start a local powershell process
	shell, err := ps.New(back)
	if err != nil {
		panic(err)
	}
	defer shell.Exit()

	// ... and interact with it
    // Can be a single command, or multiple chained with ;
    // Must not be multiline - see Excuting entire scripts below.
	stdout, stderr, err := shell.Execute("Get-WmiObject -Class Win32_Processor")
	if err != nil {
		panic(err)
	}

	fmt.Println(stdout)

    // ... or more safely
    ctx, cancel := context.WithTimeout(context.Background(), time.Second)
    defer cancel()

	stdout, stderr, err := shell.ExecuteWithContext(ctx, "Write-Host 'Hello world'")
	if err != nil {
		panic(err)
	}

	fmt.Println(stdout)

}
```

</details>

Alternatively with pwsh (PowerShell version >= 6). This will be made to work for Linux and Mac in a future version.

<details>
<summary>Expand code</summary>

```go
package main

import (

	ps "github.com/paul-at-start/go-powershell"
	"github.com/paul-at-start/go-powershell/backend"
)

func main() {
	// choose a backend, requesting pwsh
	back := &backend.Local{Version: backend.Pwsh}

	// start a local powershell process
	shell, err := ps.New(back)
	if err != nil {
		panic(err)
	}
	defer shell.Exit()
}
```

</details>

Executing entire scripts

The argument passed to `ExecuteScript` (or `ExecuteScriptWithContext`) is first examined to see if it contains newlines and if not, then via regular expression to see if it looks like a file path.

<details>
<summary>Expand code</summary>

```go
package main

import (

    "fmt"

	ps "github.com/paul-at-start/go-powershell"
	"github.com/paul-at-start/go-powershell/backend"
	"github.com/paul-at-start/go-powershell/utils"
)

func main() {
	// choose a backend, requesting pwsh
	back := &backend.Local{}

	// start a local powershell process
	shell, err := ps.New(back)
	if err != nil {
		panic(err)
	}
	defer shell.Exit()

    // script as a string
	script := `
Write-Host "hello"
Write-Host "goodbye"
`

    sout, _, err := shell.ExecuteScript(script)

    if err != nil {
        panic(err)
    }

    fmt.Println(sout)

    // script in a file - will error if file not found
    scriptFile = "this-file-must-exist.ps1"

    sout, _, err = shell.ExecuteScript(scriptFile)

    if err != nil {
        panic(err)
    }

}
```

</details>

## Remote Sessions

You can use an existing PS shell to use PSSession cmdlets to connect to remote
computers. Instead of manually handling that, you can use the Session middleware,
which takes care of authentication. Note that you can still use the "raw" shell
to execute commands on the computer where the PowerShell host process is running.

<details>
<summary>Expand code</summary>


```go
package main

import (
	"fmt"

	ps "github.com/paul-at-start/go-powershell"
	"github.com/paul-at-start/go-powershell/backend"
	"github.com/paul-at-start/go-powershell/middleware"
)

func main() {
	// choose a backend
	back := &backend.Local{}

	// start a local powershell process
	shell, err := ps.New(back)
	if err != nil {
		panic(err)
	}

	// prepare remote session configuration
	config := middleware.NewSessionConfig()
	config.ComputerName = "remote-pc-1"

	// create a new shell by wrapping the existing one in the session middleware
	session, err := middleware.NewSession(shell, config)
	if err != nil {
		panic(err)
	}
	defer session.Exit() // will also close the underlying ps shell!

	// everything run via the session is run on the remote machine
	stdout, stderr, err = session.Execute("Get-WmiObject -Class Win32_Processor")
	if err != nil {
		panic(err)
	}

	fmt.Println(stdout)
}
```

</details>

Note that all commands that you execute are wrapped in special echo
statements to delimit the stdout/stderr streams. After ``.Execute()``ing a command,
you can therefore not access ``$LastExitCode`` anymore and expect meaningful
results.

## Enhancements

The following enhancements have been made to the original code:

* Support [pwsh](https://github.com/PowerShell/PowerShell), the newer .NET Core version of PowerShell. You can choose whether to start this or regular Windows PowerShell.
* Wrap all submitted commands in a `try` block to properly capture errors and ensure the output boundary markers are properly written.
* Make the session thread safe.
* Optimize the `streamReader` function to perform fewer allocations.
* Avoid panics if `Shell.Exit` is called on a closed shell. Return an error instead.
* Add ability to pre-load modules when the shell is started.
* Added an additional shell method `ExecuteWithContext` that takes a context argument. If a shell command isn't well formed then the `stdout` and `stderr` pipes do not return anything and the `Execute` method will block indefinitely in this case. The underlying session will be restarted before `context.DeadlineExceeded` is returned to the caller. A restarted shell _may_ be unstable!
* Added an additional shell method `Version` which returns the underlying PowerShell host's version.
* Added additional shell methods `ExecuteScript` and `ExecuteScriptWithContext` to run multiline scripts or external script files.
* Sentinel errors returned by shell methods that can be tested with Errors.Is
* Ability to log interaction with the underlying PowerShell session (see [tests](./shell_logging_test.go))
* Much enlarged test suite.
* Linux support (pwsh only)

Todo:
* Verify macOS support (probably works, but you never know)


## License

MIT, see LICENSE file.
