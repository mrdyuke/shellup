// Package env detects the running operating system, the package manager and,
// on Arch Linux, a usable AUR helper. Detection is kept free of side effects
// and all external lookups are injectable so the behavior can be tested.
package env

import (
	"bufio"
	"fmt"
	"os"
	"strings"
)

// LookPathFunc resolves a program name to an absolute path, mirroring
// exec.LookPath. It is injectable for tests.
type LookPathFunc func(string) (string, error)

// Known package manager binaries in priority order. pacman wins on Arch-like
// systems, then dnf, then apt.
const (
	PMNone   = ""
	PMPacman = "pacman"
	PMDnf    = "dnf"
	PMApt    = "apt"
)

// AUR helpers to probe, in preference order. The first one found is used.
var AurHelperCandidates = []string{"yay", "paru"}

// distroInspect returns the hosts /etc/os-release path. Kept as a var so
// tests can point it at fixture files.
var distroInspect = osReleaseDefaults

// OS is the result of environment detection.
type OS struct {
	// Distro is the lowercase contents of ID in /etc/os-release.
	Distro string
	// IDLike is the raw, space-separated value of ID_LIKE, if present. It may
	// be empty. Resolution of the package manager uses both Distro and IDLike.
	IDLike string
	// PackageManager is one of the PM constants. Empty means unsupported.
	PackageManager string
	// Missing lists package manager names that could not be resolved to a
	// binary on this system, in priority order, for diagnostics.
	Missing []string
}

func osReleaseDefaults() string { return "/etc/os-release" }

// Detect resolves the running environment without side effects.
func Detect(look LookPathFunc) (OS, error) {
	return DetectFromRelease(look, distroInspect())
}

// DetectFromRelease is Detect with an explicit path to the os-release file.
func DetectFromRelease(look LookPathFunc, osReleasePath string) (OS, error) {
	release, err := ParseOSRelease(osReleasePath)
	if err != nil {
		return OS{}, err
	}
	o := ParseReleaseLookup(look, release)
	if o.PackageManager == PMNone {
		joined := strings.Join(o.Missing, ", ")
		return OS{}, fmt.Errorf(
			"none of the supported package managers (%s) are available on this system (distro: %q)",
			joined, o.Distro)
	}
	return o, nil
}

// ParseReleaseLookup resolves the package manager from a parsed os-release
// file and a LookPathFunc, without reporting errors for an unsupported distro.
func ParseReleaseLookup(look LookPathFunc, release map[string]string) OS {
	o := OS{
		Distro: strings.ToLower(release["ID"]),
		IDLike: release["ID_LIKE"],
	}
	for _, pm := range []string{PMPacman, PMDnf, PMApt} {
		if _, err := look(pm); err == nil {
			o.PackageManager = pm
			break
		}
		o.Missing = append(o.Missing, pm)
	}
	return o
}

// ParseOSRelease reads an /etc/os-release style file into a string map.
func ParseOSRelease(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), "\"'")
		out[strings.TrimSpace(key)] = value
	}
	if err := scanner.Err(); err != nil {
		return nil, err
	}
	return out, f.Close()
}

// FindAurHelper returns the first AUR helper found on PATH and its absolute
// path, or ("", "") if none is present.
func FindAurHelper(look LookPathFunc) (string, string) {
	for _, helper := range AurHelperCandidates {
		if abs, err := look(helper); err == nil {
			return helper, abs
		}
	}
	return "", ""
}
