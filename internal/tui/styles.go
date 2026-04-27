package tui

import "charm.land/lipgloss/v2"

// Fixed dark-theme palette. We avoid lipgloss.AdaptiveColor on purpose because
// adaptive colors trigger an OSC 11 query at runtime to detect the terminal
// background; that query's response was leaking back into the textarea on
// terminals that don't immediately drain it. Hardcoded colors remove the
// trigger entirely.
var (
	colPrimary   = lipgloss.Color("#FF79C6") // pink
	colSecondary = lipgloss.Color("#8BE9FD") // cyan
	colAccent    = lipgloss.Color("#BD93F9") // purple
	colSuccess   = lipgloss.Color("#50FA7B") // green
	colWarning   = lipgloss.Color("#FFB86C") // orange
	colDanger    = lipgloss.Color("#FF5555") // red
	colMuted     = lipgloss.Color("#6272A4") // grey-blue
	colText      = lipgloss.Color("#F8F8F2") // off-white
	colBorder    = lipgloss.Color("#44475A") // dark grey
	colLogoTop   = lipgloss.Color("#FF79C6") // pink (top of letters + top hatch)
	colLogoBot   = lipgloss.Color("#BD93F9") // purple (bottom of letters + bottom hatch)
	colLogoVer   = lipgloss.Color("#8BE9FD") // cyan (version label)
)

type Styles struct {
	HeaderLogo    lipgloss.Style
	HeaderTitle   lipgloss.Style
	HeaderDim     lipgloss.Style
	HeaderSep     lipgloss.Style

	UserPrompt lipgloss.Style
	UserText   lipgloss.Style

	AssistantPrompt lipgloss.Style
	AssistantText   lipgloss.Style
	AssistantThink  lipgloss.Style

	ToolPrompt lipgloss.Style
	ToolName   lipgloss.Style
	ToolInput  lipgloss.Style
	ToolResult lipgloss.Style

	ResultOK    lipgloss.Style
	ResultError lipgloss.Style
	ResultMeta  lipgloss.Style

	StatusBar  lipgloss.Style
	StatusBusy lipgloss.Style
	StatusIdle lipgloss.Style
	StatusKey  lipgloss.Style
	StatusDesc lipgloss.Style

	Input        lipgloss.Style
	InputFocused lipgloss.Style
	Hint         lipgloss.Style

	LogoTop   lipgloss.Style
	LogoBot   lipgloss.Style
	LogoVer   lipgloss.Style
	LogoBrand lipgloss.Style

	BtnSelected lipgloss.Style
	BtnPlain    lipgloss.Style

	DialogBox     lipgloss.Style
	DialogTitle   lipgloss.Style
	DialogWarning lipgloss.Style
}

func DefaultStyles() Styles {
	return Styles{
		HeaderLogo:    lipgloss.NewStyle().Foreground(colWarning).Bold(true),
		HeaderTitle:   lipgloss.NewStyle().Foreground(colPrimary).Bold(true),
		HeaderDim:     lipgloss.NewStyle().Foreground(colMuted),
		HeaderSep:     lipgloss.NewStyle().Foreground(colBorder),

		UserPrompt: lipgloss.NewStyle().Foreground(colSecondary).Bold(true),
		UserText:   lipgloss.NewStyle().Foreground(colText),

		AssistantPrompt: lipgloss.NewStyle().Foreground(colPrimary).Bold(true),
		AssistantText:   lipgloss.NewStyle().Foreground(colText),
		AssistantThink:  lipgloss.NewStyle().Foreground(colMuted).Italic(true),

		ToolPrompt: lipgloss.NewStyle().Foreground(colAccent).Bold(true),
		ToolName:   lipgloss.NewStyle().Foreground(colAccent).Bold(true),
		ToolInput:  lipgloss.NewStyle().Foreground(colMuted),
		ToolResult: lipgloss.NewStyle().Foreground(colMuted),

		ResultOK:    lipgloss.NewStyle().Foreground(colSuccess),
		ResultError: lipgloss.NewStyle().Foreground(colDanger).Bold(true),
		ResultMeta:  lipgloss.NewStyle().Foreground(colMuted),

		StatusBar:  lipgloss.NewStyle().Foreground(colMuted),
		StatusBusy: lipgloss.NewStyle().Foreground(colWarning).Bold(true),
		StatusIdle: lipgloss.NewStyle().Foreground(colSuccess),
		StatusKey:  lipgloss.NewStyle().Foreground(colText).Bold(true),
		StatusDesc: lipgloss.NewStyle().Foreground(colMuted),

		Input: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colBorder).
			Padding(0, 1),
		InputFocused: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colPrimary).
			Padding(0, 1),
		Hint: lipgloss.NewStyle().Foreground(colMuted).Italic(true),

		LogoTop:   lipgloss.NewStyle().Foreground(colLogoTop).Bold(true),
		LogoBot:   lipgloss.NewStyle().Foreground(colLogoBot).Bold(true),
		LogoVer:   lipgloss.NewStyle().Foreground(colLogoVer),
		LogoBrand: lipgloss.NewStyle().Foreground(colMuted).Italic(true),

		BtnSelected: lipgloss.NewStyle().
			Foreground(colText).
			Background(colPrimary).
			Bold(true).
			Padding(0, 2),
		BtnPlain: lipgloss.NewStyle().
			Foreground(colMuted).
			Padding(0, 2),

		DialogBox: lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(colPrimary).
			Padding(1, 2),
		DialogTitle:   lipgloss.NewStyle().Foreground(colPrimary).Bold(true),
		DialogWarning: lipgloss.NewStyle().Foreground(colWarning).Bold(true),
	}
}
