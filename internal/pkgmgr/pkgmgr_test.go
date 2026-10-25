package pkgmgr

import (
	"errors"
	"strings"
	"testing"
)

// fakeRunner records every command and answers Probe calls from a table.
type fakeRunner struct {
	probeResult map[string]error // "probe <cmd> <args...>" -> nil or failure
	probeOutput map[string]string
	streamed    []string // joined "cmd args..." of Stream calls
	stdined     []string // joined "cmd args..." of StreamStdin calls
}

func (f *fakeRunner) Probe(name string, args ...string) (string, error) {
	key := debugKey("probe", name, args)
	err := f.probeResult[key]
	if err == nil && f.probeResult == nil {
		err = errors.New("probe failed")
	}
	return f.probeOutput[key], err
}

func (f *fakeRunner) Stream(name string, args ...string) error {
	f.streamed = append(f.streamed, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func (f *fakeRunner) StreamStdin(name string, args []string, stdin string) error {
	f.stdined = append(f.stdined, strings.Join(append([]string{name}, args...), " "))
	return nil
}

func debugKey(prefix, name string, args []string) string {
	return strings.Join(append([]string{prefix, name}, args...), " ")
}

func newRunner() *fakeRunner {
	return &fakeRunner{probeResult: map[string]error{}, probeOutput: map[string]string{}}
}

func TestPacmanInstallUsesNeededFlag(t *testing.T) {
	runner := newRunner()
	pm, err := NewWithRunner("pacman", runner, "sudo")
	if err != nil {
		t.Fatal(err)
	}
	if err := pm.Install("fish", "fzf"); err != nil {
		t.Fatal(err)
	}
	if len(runner.streamed) != 1 {
		t.Fatalf("want one streamed command, got %d", len(runner.streamed))
	}
	if !strings.Contains(runner.streamed[0], "sudo pacman -S --needed fish fzf") {
		t.Fatalf("unexpected command: %q", runner.streamed[0])
	}
}

func TestPacmanInstalledFiltersMissing(t *testing.T) {
	runner := newRunner()
	runner.probeResult["probe pacman -Q fish"] = nil // installed ok
	runner.probeResult["probe pacman -Q fzf"] = errors.New("not found")
	pm, _ := NewWithRunner("pacman", runner, "sudo")
	missing, err := pm.Installed("fish", "fzf")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "fzf" {
		t.Fatalf("want [fzf], got %v", missing)
	}
}

func TestAptInstallRunsUpdateThenInstallY(t *testing.T) {
	runner := newRunner()
	pm, _ := NewWithRunner("apt", runner, "sudo")
	if err := pm.Refresh(); err != nil {
		t.Fatal(err)
	}
	if err := pm.Install("fish"); err != nil {
		t.Fatal(err)
	}
	joined := strings.Join(runner.streamed, "\n")
	if !strings.Contains(joined, "sudo apt-get update") ||
		!strings.Contains(joined, "sudo apt-get install -y fish") {
		t.Fatalf("unexpected command sequence: %q", joined)
	}
}

func TestAptInstalledRequiresExplicitInstalledStatus(t *testing.T) {
	runner := newRunner()
	runner.probeOutput["probe dpkg-query -W -f=${db:Status-Status} fish"] = "installed"
	runner.probeOutput["probe dpkg-query -W -f=${db:Status-Status} eza"] = "config-files"
	pm, _ := NewWithRunner("apt", runner, "sudo")
	missing, err := pm.Installed("fish", "eza")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(missing, ",") != "eza" {
		t.Fatalf("want [eza], got %v", missing)
	}
}

func TestAptAvailableUsesAptCacheShow(t *testing.T) {
	runner := newRunner()
	runner.probeResult["probe apt-cache show eza"] = nil
	runner.probeOutput["probe apt-cache show eza"] = "Package: eza\n"
	pm, _ := NewWithRunner("apt", runner, "sudo")
	found, err := pm.Available("eza", "btop")
	if err != nil {
		t.Fatal(err)
	}
	if strings.Join(found, ",") != "eza" {
		t.Fatalf("want [eza], got %v", found)
	}
}

func TestDnfInstallUsesDashDashY(t *testing.T) {
	runner := newRunner()
	pm, _ := NewWithRunner("dnf", runner, "sudo")
	if err := pm.Install("fish"); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(strings.Join(runner.streamed, "\n"), "sudo dnf install -y fish") {
		t.Fatalf("unexpected command: %q", runner.streamed)
	}
}

func TestAURHelperWrapsHelper(t *testing.T) {
	runner := newRunner()
	pm := NewAUR("paru")
	if pm.Name() != "aur (paru)" {
		t.Fatalf("unexpected helper name %q", pm.Name())
	}
	all, err := pm.Available("eza")
	if err != nil || len(all) != 1 {
		t.Fatalf("helper availability should pass through, got %v %v", all, err)
	}
	_ = pm
	_ = runner
}

func TestNewRejectsUnknownKind(t *testing.T) {
	if _, err := NewWithRunner("portage", newRunner(), "sudo"); err == nil {
		t.Fatal("want error for unknown package manager")
	}
}

func TestNamesForPerDistro(t *testing.T) {
	if got := NamesFor("pacman", Required); len(got.Missing) != 0 || got.Installed["fish"] != "fish" {
		t.Fatalf("unexpected arch mapping: %#v", got)
	}
	got := NamesFor("apt", Required)
	if got.Installed["fish"] != "fish" {
		t.Fatalf("unexpected debian fish mapping: %#v", got)
	}
	if len(got.Missing) != 0 {
		t.Fatalf("debian table should cover every required tool: %v", got.Missing)
	}
	if got := NamesFor("dnf", Required); len(got.Missing) != 0 {
		t.Fatalf("fedora table should cover every required tool: %v", got.Missing)
	}
}

func TestVersionAtLeast(t *testing.T) {
	cases := []struct {
		version, want string
		expected      bool
	}{
		{"0.18.9-1", "0.10", true},
		{"0.9-1", "0.10", false},
		{"1.0", "1.0", true},
		{"2.0-1", "1.99", true},
		{"0.8.7", "0.10", false},
	}
	for _, c := range cases {
		if got := VersionAtLeast(c.version, c.want); got != c.expected {
			t.Errorf("VersionAtLeast(%q, %q) = %v, want %v", c.version, c.want, got, c.expected)
		}
	}
}

func TestParseAptCandidate(t *testing.T) {
	policy := "eza:\n  Installed: (none)\n  Candidate: 0.18.9-1\n"
	if got := ParseAptCandidate(policy); got != "0.18.9-1" {
		t.Fatalf("want 0.18.9-1, got %q", got)
	}
	if got := ParseAptCandidate("Package: nothing\n"); got != "" {
		t.Fatalf("want empty, got %q", got)
	}
}
