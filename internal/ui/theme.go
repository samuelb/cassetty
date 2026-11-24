package ui

import (
	"image/color"

	"charm.land/lipgloss/v2"
)

// Theme is the full palette of the player.
type Theme struct {
	Name string

	Bg          color.Color // app background
	Border      color.Color // panel borders
	BorderFocus color.Color
	Text        color.Color
	Muted       color.Color
	Faint       color.Color
	Accent      color.Color // primary highlight
	Accent2     color.Color // secondary highlight
	OnAccent    color.Color // text drawn on Accent
	SelBg       color.Color // list cursor background
	HoverBg     color.Color
	Error       color.Color

	LCDBg    color.Color
	LCDFg    color.Color
	LCDText  color.Color // secondary LCD text
	LCDGhost color.Color // unlit LCD segments
	LCDFrame color.Color
	SpecLow  color.Color
	SpecMid  color.Color
	SpecHigh color.Color
	SpecPeak color.Color

	ShellEdge  color.Color
	ShellBg    color.Color
	LabelBg    color.Color
	LabelText  color.Color
	LabelMuted color.Color
	Stripe1    color.Color
	Stripe2    color.Color
	WindowBg   color.Color
	Tape       color.Color
	Hub        color.Color

	BtnBg    color.Color
	BtnFg    color.Color
	BtnHover color.Color

	// Optional multi-color ramps (a rainbow) replacing the default blends.
	// Band paints one label stripe per window row instead of blending
	// Stripe1 into Stripe2. Meter runs along the analyzer (coloring each bar
	// by frequency), the VU meters and the volume wedge, instead of coloring
	// them by level with SpecLow, SpecMid and SpecHigh.
	Band  []color.Color
	Meter []color.Color

	// Light marks a light faceplate. Such a theme always paints the terminal
	// background, as its dark text would vanish on a dark one.
	Light bool
}

var c = lipgloss.Color

var themes = []Theme{
	{
		Name: "classic",
		Bg:   c("#0f0f12"), Border: c("#34323a"), BorderFocus: c("#f5a524"),
		Text: c("#e8e4dc"), Muted: c("#8f8a96"), Faint: c("#4a4752"),
		Accent: c("#f5a524"), Accent2: c("#5ee6a8"), OnAccent: c("#1a1408"),
		SelBg: c("#27242e"), HoverBg: c("#1c1a21"), Error: c("#ff5c57"),
		LCDBg: c("#050a07"), LCDFg: c("#3dff8a"), LCDText: c("#2bc86d"), LCDGhost: c("#0f2618"), LCDFrame: c("#1f3a2a"),
		SpecLow: c("#1fd35b"), SpecMid: c("#e6d81e"), SpecHigh: c("#ff3b30"), SpecPeak: c("#d8efe0"),
		ShellEdge: c("#6e6a75"), ShellBg: c("#1d1c22"), LabelBg: c("#ede3c8"), LabelText: c("#1e1b18"), LabelMuted: c("#7a6f5c"),
		Stripe1: c("#e2522f"), Stripe2: c("#f2a922"), WindowBg: c("#0b0a0d"), Tape: c("#9a6238"), Hub: c("#f2f0ea"),
		BtnBg: c("#25232b"), BtnFg: c("#d8d3cb"), BtnHover: c("#33303b"),
	},
	{
		Name: "amber",
		Bg:   c("#100c08"), Border: c("#3a2f22"), BorderFocus: c("#ffb000"),
		Text: c("#f3e6d0"), Muted: c("#a08a6a"), Faint: c("#4d3f2c"),
		Accent: c("#ffb000"), Accent2: c("#ff7a1a"), OnAccent: c("#1a1002"),
		SelBg: c("#2a2116"), HoverBg: c("#1d1710"), Error: c("#ff5c57"),
		LCDBg: c("#120a02"), LCDFg: c("#ffb000"), LCDText: c("#d68f00"), LCDGhost: c("#2a1c05"), LCDFrame: c("#4a3208"),
		SpecLow: c("#b86e00"), SpecMid: c("#ffb000"), SpecHigh: c("#ffe08a"), SpecPeak: c("#fff1c9"),
		ShellEdge: c("#7a6a55"), ShellBg: c("#211a12"), LabelBg: c("#f1e4c6"), LabelText: c("#241a0e"), LabelMuted: c("#86704d"),
		Stripe1: c("#7a3b12"), Stripe2: c("#e08a1e"), WindowBg: c("#0d0905"), Tape: c("#8a5a2e"), Hub: c("#f6efe2"),
		BtnBg: c("#2a2116"), BtnFg: c("#efdcbc"), BtnHover: c("#3a2e1f"),
	},
	{
		Name: "vfd",
		Bg:   c("#0b1013"), Border: c("#253238"), BorderFocus: c("#59e3f0"),
		Text: c("#e2eef0"), Muted: c("#7f9499"), Faint: c("#3a4a50"),
		Accent: c("#59e3f0"), Accent2: c("#ff6f91"), OnAccent: c("#031214"),
		SelBg: c("#16242a"), HoverBg: c("#111b1f"), Error: c("#ff5c57"),
		LCDBg: c("#031012"), LCDFg: c("#5ff6e8"), LCDText: c("#36c9c0"), LCDGhost: c("#0a2427"), LCDFrame: c("#16383c"),
		SpecLow: c("#1b8fc9"), SpecMid: c("#4ff2e6"), SpecHigh: c("#e7fffd"), SpecPeak: c("#ff6f91"),
		ShellEdge: c("#7c8a90"), ShellBg: c("#1a2024"), LabelBg: c("#eef1f2"), LabelText: c("#0f1a24"), LabelMuted: c("#5d7080"),
		Stripe1: c("#2f6fdb"), Stripe2: c("#59e3f0"), WindowBg: c("#090c0e"), Tape: c("#7d5a3c"), Hub: c("#f5f8f8"),
		BtnBg: c("#1a252a"), BtnFg: c("#d3e3e6"), BtnHover: c("#243338"),
	},
	{
		Name: "synthwave",
		Bg:   c("#130e1d"), Border: c("#3a2c52"), BorderFocus: c("#ff5fd2"),
		Text: c("#f1e9ff"), Muted: c("#9c8bb8"), Faint: c("#4c3d66"),
		Accent: c("#ff5fd2"), Accent2: c("#7be0ff"), OnAccent: c("#1d0718"),
		SelBg: c("#2a1f3d"), HoverBg: c("#1d152b"), Error: c("#ff5c57"),
		LCDBg: c("#0e0618"), LCDFg: c("#ff5fd2"), LCDText: c("#c94fb0"), LCDGhost: c("#2a0f33"), LCDFrame: c("#3e1d52"),
		SpecLow: c("#7b5cff"), SpecMid: c("#ff5fd2"), SpecHigh: c("#ffd36e"), SpecPeak: c("#7be0ff"),
		ShellEdge: c("#7d6a9c"), ShellBg: c("#1f1730"), LabelBg: c("#f6e9ff"), LabelText: c("#2a1240"), LabelMuted: c("#86679f"),
		Stripe1: c("#ff5fd2"), Stripe2: c("#7be0ff"), WindowBg: c("#0c0814"), Tape: c("#8a5a7a"), Hub: c("#fdf6ff"),
		BtnBg: c("#271d39"), BtnFg: c("#e6dbf7"), BtnHover: c("#34274b"),
	},
	{
		// A rainbow tape that spent a summer on the dashboard.
		Name: "sunbleached",
		Bg:   c("#1d1b21"), Border: c("#3b3640"), BorderFocus: c("#e8a87c"),
		Text: c("#f0e7dc"), Muted: c("#a39a9f"), Faint: c("#514a55"),
		Accent: c("#e8a87c"), Accent2: c("#b49fd3"), OnAccent: c("#2a1a10"),
		SelBg: c("#302b35"), HoverBg: c("#26222a"), Error: c("#e2726b"),
		LCDBg: c("#141217"), LCDFg: c("#f3e3c3"), LCDText: c("#b9aa92"), LCDGhost: c("#2a2530"), LCDFrame: c("#3d3644"),
		SpecLow: c("#a6c99a"), SpecMid: c("#e8d08a"), SpecHigh: c("#e38b86"), SpecPeak: c("#f6efe6"),
		ShellEdge: c("#8f8594"), ShellBg: c("#2a2630"), LabelBg: c("#f0e6d2"), LabelText: c("#3b302b"), LabelMuted: c("#8d7d6d"),
		Stripe1: c("#e38b86"), Stripe2: c("#8fb8d6"), WindowBg: c("#151318"), Tape: c("#8f6a50"), Hub: c("#f5eee4"),
		BtnBg: c("#2b2730"), BtnFg: c("#e3d9cf"), BtnHover: c("#38323d"),
		Band:  []color.Color{c("#e38b86"), c("#eaa978"), c("#e8d08a"), c("#a6c99a"), c("#8fb8d6")},
		Meter: []color.Color{c("#b49fd3"), c("#8fb8d6"), c("#a6c99a"), c("#e8d08a"), c("#eaa978"), c("#e38b86")},
	},
	{
		// Brushed aluminium front, piano keys and a warmly lit meter window.
		Name: "silverface",
		Bg:   c("#d2d5d8"), Border: c("#9a9ea4"), BorderFocus: c("#4a4e55"),
		Text: c("#1c1e21"), Muted: c("#4f545b"), Faint: c("#8b9097"),
		Accent: c("#b3301c"), Accent2: c("#1f5a9e"), OnAccent: c("#fff8f2"),
		SelBg: c("#babec3"), HoverBg: c("#c6c9cd"), Error: c("#b3261e"),
		LCDBg: c("#efd8a0"), LCDFg: c("#1d1a14"), LCDText: c("#5b4a2a"), LCDGhost: c("#dcc386"), LCDFrame: c("#4a4e55"),
		SpecLow: c("#2a2620"), SpecMid: c("#2a2620"), SpecHigh: c("#c42b1c"), SpecPeak: c("#c42b1c"),
		ShellEdge: c("#6a6e75"), ShellBg: c("#2b2d31"), LabelBg: c("#f6f5f0"), LabelText: c("#1a1a1a"), LabelMuted: c("#6b6b6b"),
		Stripe1: c("#1f5a9e"), Stripe2: c("#9fb4cc"), WindowBg: c("#121316"), Tape: c("#6b4a33"), Hub: c("#f2f2f2"),
		BtnBg: c("#eceef0"), BtnFg: c("#26292d"), BtnHover: c("#f8f9fa"),
		Light: true,
	},
	{
		// Acid yellow, hot magenta and electric cyan on black glass.
		Name: "cyberdeck",
		Bg:   c("#07080c"), Border: c("#1c2833"), BorderFocus: c("#fcee0a"),
		Text: c("#dff9ff"), Muted: c("#6f8d99"), Faint: c("#2b3a44"),
		Accent: c("#fcee0a"), Accent2: c("#00f0ff"), OnAccent: c("#050505"),
		SelBg: c("#10202a"), HoverBg: c("#0c161d"), Error: c("#ff2a6d"),
		LCDBg: c("#0a0306"), LCDFg: c("#ff2a50"), LCDText: c("#d11f43"), LCDGhost: c("#2b070f"), LCDFrame: c("#0e5a63"),
		SpecLow: c("#00f0ff"), SpecMid: c("#ff2a6d"), SpecHigh: c("#fcee0a"), SpecPeak: c("#ffffff"),
		ShellEdge: c("#00f0ff"), ShellBg: c("#0e1117"), LabelBg: c("#fcee0a"), LabelText: c("#0a0a0a"), LabelMuted: c("#6b6400"),
		Stripe1: c("#ff2a6d"), Stripe2: c("#00f0ff"), WindowBg: c("#050608"), Tape: c("#5a3d4f"), Hub: c("#00f0ff"),
		BtnBg: c("#11161e"), BtnFg: c("#8fe9f3"), BtnHover: c("#1b2430"),
		Band: []color.Color{c("#ff2a6d"), c("#9b30ff"), c("#00f0ff")},
	},
}

// DefaultTheme is used when neither the flag, the config nor the last run
// name a theme.
const DefaultTheme = "sunbleached"

// themeIndex returns the index of the named theme, or of DefaultTheme when
// the name is empty or unknown.
func themeIndex(name string) int {
	def := 0
	for i, t := range themes {
		if t.Name == name {
			return i
		}
		if t.Name == DefaultTheme {
			def = i
		}
	}
	return def
}

// ThemeNames lists the available themes.
func ThemeNames() []string {
	names := make([]string, len(themes))
	for i, t := range themes {
		names[i] = t.Name
	}
	return names
}
