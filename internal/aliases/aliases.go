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
	// Snippet is the fish statement added to conf.d/shellup.fish when the
	// answer is yes.
	Snippet string
}

// CatExplanation explains why bat is aliased without paging: plain `bat` with
// a pager breaks pipes such as `cat file | grep pattern`.
const CatExplanation = "bat normally pages; --paging=never keeps pipes like `cat file | grep` working."

// Options is the asked-in-order list of optional integrations.
var Options = []Option{
	{
		Key:         "cd-zoxide",
		Question:    "Make cd smart with zoxide? [Y/n]",
		Explanation: "zoxide learns the directories you actually visit and cd learns from them.",
		DefaultYes:  true,
		Snippet:     "zoxide init fish --cmd cd | source",
	},
	{
		Key:         "cat-bat",
		Question:    "Alias cat to bat? [Y/n]",
		Explanation: CatExplanation,
		DefaultYes:  true,
		Snippet:     "alias cat 'bat --paging=never'",
	},
	{
		Key:         "ls-eza",
		Question:    "Alias ls to eza? [Y/n]",
		Explanation: "eza prints colored listings; --icons adds Nerd Font glyphs (needs an icon font).",
		DefaultYes:  false,
		Snippet:     "alias ls 'eza --icons'",
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
