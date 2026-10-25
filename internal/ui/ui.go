// Package ui implements the interactive dialogs of Shellup on
// charmbracelet/bubbletea with lipgloss styling: yes/no confirmations and
// single-choice selects. Critical questions never fall back to plain
// fmt.Scanln parsing.
package ui

import (
	"errors"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// ErrAborted is returned when the user quits a dialog instead of answering.
// Callers must treat it as "no change": exit without touching the system.
var ErrAborted = errors.New("user aborted the interactive dialog")

var (
	titleStyle    = lipgloss.NewStyle().Bold(true)
	selectedStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("10"))
	normalStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("7"))
	dimStyle      = lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
)

// Item is one selectable entry of a Select dialog.
type Item struct {
	Label       string
	Description string
}

type confirmModel struct {
	question    string
	explanation string
	defaultYes  bool
	cursor      int // 0 = first option
	options     [2]string
	choiceCh    chan tea.Msg
}

type confirmDoneMsg struct{ value bool }

func newConfirmModel(question, explanation string, defaultYes bool) confirmModel {
	opts := [2]string{"No", "Yes"}
	cursor := 0
	if defaultYes {
		opts = [2]string{"Yes", "No"}
	}
	return confirmModel{question: question, explanation: explanation, defaultYes: defaultYes, cursor: cursor, options: opts}
}

func (m confirmModel) Init() tea.Cmd { return nil }

func (m confirmModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "y", "Y":
			return m, func() tea.Msg { return confirmDoneMsg{value: true} }
		case "n", "N":
			return m, func() tea.Msg { return confirmDoneMsg{value: false} }
		case "left", "right", "tab", "h", "l", "space":
			m.cursor++
			m.cursor %= 2
			return m, nil
		case "up", "k":
			m.cursor++
			m.cursor %= 2
			return m, nil
		case "down", "j":
			m.cursor++
			m.cursor %= 2
			return m, nil
		case "enter":
			return m, func() tea.Msg {
				return confirmDoneMsg{value: m.options[m.cursor] == "Yes"}
			}
		}
	case confirmDoneMsg:
		if m.choiceCh != nil {
			m.choiceCh <- msg
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m confirmModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.question))
	b.WriteString("\n")
	if m.explanation != "" {
		b.WriteString(dimStyle.Render(strings.Join(wrap(m.explanation, 76), "\n")))
		b.WriteString("\n")
	}
	for i, opt := range m.options {
		style := normalStyle
		marker := "( ) "
		if i == m.cursor {
			style = selectedStyle
			marker = "(*) "
		}
		b.WriteString(marker + style.Render(opt) + "   ")
	}
	b.WriteString("\n" + dimStyle.Render("Left/Right/Tab to switch, Enter to confirm, y/n for a shortcut, Esc aborts without changes."))
	return b.String()
}

// Confirm asks a yes/no question and returns the answer. It uses the
// terminal (bubbletea), never a raw Scanln. Values: answer, abort.
func Confirm(question, explanation string, defaultYes bool) (bool, error) {
	ch := make(chan tea.Msg, 1)
	model := newConfirmModel(question, explanation, defaultYes)
	model.choiceCh = ch
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return false, err
	}
	msg := <-ch
	if m, ok := msg.(confirmDoneMsg); ok {
		return m.value, nil
	}
	return false, ErrAborted
}

type selectModel struct {
	title    string
	items    []Item
	cursor   int
	choiceCh chan tea.Msg
}

type selectDoneMsg struct{ index int }

func (m selectModel) Init() tea.Cmd { return nil }

func (m selectModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "esc", "ctrl+c":
			return m, tea.Quit
		case "up", "k":
			m.cursor = (m.cursor - 1 + len(m.items)) % len(m.items)
			return m, nil
		case "down", "j", "tab":
			m.cursor = (m.cursor + 1) % len(m.items)
			return m, nil
		case "enter":
			return m, func() tea.Msg { return selectDoneMsg{index: m.cursor} }
		}
	case selectDoneMsg:
		if m.choiceCh != nil {
			m.choiceCh <- msg
		}
		return m, tea.Quit
	}
	return m, nil
}

func (m selectModel) View() string {
	var b strings.Builder
	b.WriteString(titleStyle.Render(m.title))
	b.WriteString("\n")
	for i, item := range m.items {
		line := fmtItem(item.Label, item.Description)
		if i == m.cursor {
			b.WriteString(selectedStyle.Render("> "+line) + "\n")
			continue
		}
		b.WriteString("  " + normalStyle.Render(line) + "\n")
	}
	b.WriteString(dimStyle.Render("Up/Down to move, Enter to select, Esc aborts without changes."))
	return b.String()
}

func fmtItem(label, description string) string {
	if description == "" {
		return label
	}
	return label + "  " + description
}

// Select shows a single-choice list and returns the chosen index.
func Select(title string, items []Item) (int, error) {
	ch := make(chan tea.Msg, 1)
	model := selectModel{title: title, items: items, choiceCh: ch}
	p := tea.NewProgram(model)
	if _, err := p.Run(); err != nil {
		return 0, err
	}
	msg := <-ch
	if m, ok := msg.(selectDoneMsg); ok {
		return m.index, nil
	}
	return 0, ErrAborted
}

// wrap is a tiny word wrapper to keep explanation lines readable on narrow
// terminals without pulling an extra dependency.
func wrap(s string, width int) []string {
	words := strings.Fields(s)
	if len(words) == 0 {
		return []string{s}
	}
	var lines []string
	line := words[0]
	for _, w := range words[1:] {
		if len(line)+len(w)+1 > width {
			lines = append(lines, line)
			line = w
			continue
		}
		line += " " + w
	}
	return append(lines, line)
}
