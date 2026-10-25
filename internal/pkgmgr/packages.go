package pkgmgr

import (
	"fmt"
	"strconv"
	"strings"
)

// Spec maps a logical tool name onto distribution package names. An empty
// package name means "not available in that distribution's repositories";
// the caller is expected to surface a fallback or skip with a clear message.
type Spec struct {
	Key    string // logical name, e.g. "fzf"
	Arch   string // pacman / AUR package
	Debian string // apt package
	Fedora string // dnf package
}

// Required lists the tools Shellup installs for its core experience. The table
// is the single source of truth; nothing is spread across the codebase.
var Required = []Spec{
	{Key: "fish", Arch: "fish", Debian: "fish", Fedora: "fish"},
	{Key: "fzf", Arch: "fzf", Debian: "fzf", Fedora: "fzf"},
	{Key: "eza", Arch: "eza", Debian: "eza", Fedora: "eza"},
	{Key: "bat", Arch: "bat", Debian: "bat", Fedora: "bat"},
	{Key: "zoxide", Arch: "zoxide", Debian: "zoxide", Fedora: "zoxide"},
	{Key: "btop", Arch: "btop", Debian: "btop", Fedora: "btop"},
}

// PromptStarship is installed only when the user picks starship as prompt.
var PromptStarship = Spec{Key: "starship", Arch: "starship", Debian: "starship", Fedora: "starship"}

// ParseAptCandidate extracts the version from `apt-cache policy` output,
// falling back to the Candidate line value even when it is "(none)".
func ParseAptCandidate(policy string) string {
	for _, line := range strings.Split(policy, "\n") {
		line = strings.TrimSpace(line)
		if rest, ok := strings.CutPrefix(line, "Candidate:"); ok {
			return strings.TrimSpace(rest)
		}
	}
	return ""
}

// VersionAtLeast compares dotted numeric versions such as "0.18.9-1" against
// a wanted baseline like "0.10". Non-numeric suffixes after a component are
// ignored. It is used to warn about outdated eza packages on Debian/Ubuntu.
func VersionAtLeast(version, want string) bool {
	vParts := strings.FieldsFunc(version, isDotOrDash)
	wParts := strings.FieldsFunc(want, isDotOrDash)
	for i := 0; i < len(vParts) || i < len(wParts); i++ {
		vn := numericPrefix(vParts, i)
		wn := numericPrefix(wParts, i)
		if vn != wn {
			return vn > wn
		}
	}
	return true
}

// numericPrefix parses the leading digits of component i, returning 0 when it
// is absent or non-numeric.
func numericPrefix(parts []string, i int) int {
	if i >= len(parts) {
		return 0
	}
	digits := parts[i][:spanDigits(parts[i])]
	n, err := strconv.Atoi(digits)
	if err != nil {
		return 0
	}
	return n
}

func spanDigits(s string) int {
	n := 0
	for n < len(s) && s[n] >= '0' && s[n] <= '9' {
		n++
	}
	return n
}

func isDotOrDash(r rune) bool { return r == '.' || r == '-' }

// DescribeMissing renders a user-facing message for the keys that have no
// package on the running distribution.
func DescribeMissing(distroName string, keys []string) string {
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("The following tools have no package in the %s repositories and will be skipped: %s",
		distroName, strings.Join(keys, ", "))
}
