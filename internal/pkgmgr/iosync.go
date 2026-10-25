package pkgmgr

import "os"

// osStdin, osStdout and osStderr return the process terminal streams. They
// are variables so tests can keep Exec from touching the test terminal.
var (
	osStdin  = func() *os.File { return os.Stdin }
	osStdout = func() *os.File { return os.Stdout }
	osStderr = func() *os.File { return os.Stderr }
)
