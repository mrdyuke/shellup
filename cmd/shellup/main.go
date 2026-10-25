// Command shellup is an interactive CLI that sets up a fish-based developer
// environment: installs shell tools, configures the prompt, adds aliases and
// switches the default shell. It never touches existing configuration without
// a backup and explicit user consent.
package main

import (
	"fmt"
	"os"
)

// version can be overridden at build time with -ldflags.
var version = "dev"

func main() {
	fmt.Printf("shellup %s\n", version)
	os.Exit(0)
}
