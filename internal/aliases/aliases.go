// Package aliases defines the optional aliases Shellup can enable and renders
// them as fish statements. Every alias is opt-in through its own interactive
// question shown to the user; nothing is enabled silently.
package aliases

// Option is one optional integration offered as its own yes/no question.
type Option struct {
	// Key is a stable identifier, e.g. "cd-zoxide" or "cat-bat".
	Key string
	// Question is the exact wording of the confirm dialog.
	Question string
	// Explanation is a short reason shown under the question, so users accept
	// or decline the change knowing what it does.
	Explanation string
	// DefaultYes marks the preselected state in the confirm dialog.
	DefaultYes bool
	// Snippet is the fish source added to conf.d/shellup.fish when the
	// answer is yes; multi-line function definitions are allowed.
	Snippet string
} // CDFallbackFunction renders the fish function backing the cd option. It
// keeps the standard cd behavior and only consults zoxide when the plain
// argument is not an existing directory, so unknown paths never break:
//
//   - no arguments: builtin cd to $HOME
//   - a single dash: prevd/nextd, fish's directory history primitives
//   - an existing directory: builtin cd to it directly
//   - otherwise: ask zoxide interactively and cd to the returned path
//   - on failure: a clear error on stderr with a non-zero exit status
//
// The builtin cd does not maintain $dirprev/$dirnext; the stock fish cd
// function does. Since this override replaces the stock function, the history
// bookkeeping from the stock implementation is mirrored, so `cd -` keeps
// working.
func CDFallbackFunction() string {
	return `function cd --description 'cd with zoxide fallback'
    # Skip history bookkeeping in command substitutions, like the stock cd.
    if status is-command-substitution
        builtin cd $argv
        return $status
    end

    set -l previous $PWD

    if test (count $argv) -eq 0
        builtin cd $HOME
        set -l st $status
        if test $st -eq 0
            __shellup_cd_history $previous
        end
        return $st
    end

    if test "$argv[1]" = -
        if test "$__fish_cd_direction" = next
            nextd
        else
            prevd
        end
        return $status
    end

    if test -d "$argv[1]"
        builtin cd $argv
        set -l st $status
        if test $st -eq 0
            __shellup_cd_history $previous
        end
        return $st
    end

    set -l zpath (zoxide query -i -- $argv 2>/dev/null)
    if test -n "$zpath"; and test -d "$zpath"
        builtin cd "$zpath"
        set -l st $status
        if test $st -eq 0
            __shellup_cd_history $previous
        end
        return $st
    end

    echo "cd: no such file or directory: $argv[1]" >&2
    return 1
end

# __shellup_cd_history mirrors the directory history bookkeeping of the stock
# fish cd function so prevd/nextd keep working while cd is overridden here.
function __shellup_cd_history --description 'record cd directory history'
    if test "$PWD" = "$argv[1]"
        return
    end
    if set -q dirprev[25]
        set -e dirprev[1]
    end
    set -U -q dirprev
    and set -U -a dirprev $argv[1]
    or set -g -a dirprev $argv[1]

    set -U -q dirnext
    and set -U -e dirnext
    or set -e dirnext

    set -U -q __fish_cd_direction
    and set -U __fish_cd_direction prev
    or set -g __fish_cd_direction prev
end
`
}

// CatExplanation explains why bat is aliased without paging: plain `bat` with
// a pager breaks pipes such as `cat file | grep pattern`.
const CatExplanation = "bat normally pages; --paging=never keeps pipes like `cat file | grep` working."

// Options is the asked-in-order list of optional integrations.
var Options = []Option{
	{
		Key:      "cd-zoxide",
		Question: "Make cd smart with zoxide? [Y/n]",
		Explanation: "zoxide learns the directories you actually visit; the wrapped cd " +
			"still falls back to the builtin cd for ordinary directory changes.",
		DefaultYes: true,
		Snippet:    CDFallbackFunction(),
	},
	{
		Key:         "cat-bat",
		Question:    "Alias cat to bat? [Y/n]",
		Explanation: CatExplanation,
		DefaultYes:  true,
		Snippet:     "alias cat 'bat --paging=never'",
	},
	{
		Key:      "ls-eza",
		Question: "Alias ls to eza? [Y/n]",
		Explanation: "eza prints colored listings; --icons=always keeps Nerd Font glyphs on " +
			"(needs an icon font). The explicit value also stops recent eza versions from " +
			"swallowing the first path argument.",
		DefaultYes: false,
		Snippet:    "alias ls 'eza --icons=always'",
	},
}

// ByKey returns the option with the given key or false. Used by the app to
// map TUI answers back onto Options entries without index math.
func ByKey(key string) (Option, bool) {
	for _, o := range Options {
		if o.Key == key {
			return o, true
		}
	}
	return Option{}, false
}

// RenderStatements builds the alias section lines in the canonical option
// order. Chosen is a set of keys answering yes; every other key is skipped
// entirely, so a declined question leaves stock fish behavior untouched.
func RenderStatements(chosen map[string]bool) []string {
	var out []string
	for _, o := range Options {
		if chosen[o.Key] {
			out = append(out, o.Snippet)
		}
	}
	return out
}
