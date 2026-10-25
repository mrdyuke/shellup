package env

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestParseOSRelease(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "os-release")
	content := "ID=\"arch\"\nNAME=\"Arch Linux\"\n# a comment\n\nPRETTY_NAME='Arch'\n"
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	got, err := ParseOSRelease(path)
	if err != nil {
		t.Fatal(err)
	}
	if got["ID"] != "arch" || got["PRETTY_NAME"] != "Arch" || len(got) != 3 {
		t.Fatalf("unexpected parse result: %#v", got)
	}
	_, err = ParseOSRelease(filepath.Join(dir, "missing"))
	if !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("want os.ErrNotExist, got %v", err)
	}
}

func TestPackageManagerPriorityOrder(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "dnf" || name == "apt" {
			return "/usr/bin/" + name, nil
		}
		return "", &os.PathError{Op: "lookup", Err: errors.New("no such file")}
	}
	o := ParseReleaseLookup(look, map[string]string{"ID": "fedora"})
	if o.PackageManager != PMDnf || len(o.Missing) != 1 || o.Missing[0] != "pacman" {
		t.Fatalf("unexpected detection result: %#v", o)
	}
}

func TestFindAurHelper(t *testing.T) {
	look := func(name string) (string, error) {
		if name == "paru" {
			return "/usr/bin/paru", nil
		}
		return "", &os.PathError{Op: "lookup", Err: errors.New("no such file")}
	}
	name, abs := FindAurHelper(look)
	if name != "paru" || abs != "/usr/bin/paru" {
		t.Fatalf("want paru, got %q %q", name, abs)
	}
	name, abs = FindAurHelper(func(string) (string, error) {
		return "", &os.PathError{Op: "lookup", Err: errors.New("no such file")}
	})
	if name != "" || abs != "" {
		t.Fatalf("want empty result, got %q %q", name, abs)
	}
}

func TestDetectFromReleaseFailsWithoutPackageManager(t *testing.T) {
	_, err := DetectFromRelease(func(string) (string, error) {
		return "", &os.PathError{Op: "lookup", Err: errors.New("no such file")}
	}, "/nonexistent/os-release")
	if err == nil {
		t.Fatal("want error for missing os-release")
	}
}
