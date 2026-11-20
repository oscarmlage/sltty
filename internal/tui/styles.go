package tui

import "charm.land/lipgloss/v2"

// The styles of the current theme. applyTheme sets them all; the app has one
// screen, so they are package state.
var (
	theme Theme

	paneBg, surfaceBg, sbBg, sbSelBg, sbSelFocusBg, selBg, dragSelBg string // background sequences

	sbTitle, sbMuted, sbBright, sbSelText, sbOnline, sbBadge, sbSelBadge, sbMark, sbDot lipgloss.Style
	sbBadgeMuted                                                                        lipgloss.Style

	headerStyle, headerFocus, nameStyle, meStyle, textStyle, dimStyle, linkStyle lipgloss.Style
	reactStyle, reactMine, dayStyle, ruleStyle, selMark, openMark, keyStyle      lipgloss.Style
	errStyle, editTitle, titleStyle, keyCap, helpGroup                           lipgloss.Style

	normalPill, insertPill, statusWhere, offlinePill   lipgloss.Style
	dividerStyle, dragStyle, boxBorder, boxBorderFocus lipgloss.Style

	newRule, newStyle, scopeChip, commandPill, onDot, spinStyle lipgloss.Style

	modalBorder, modalTitle, modalInput, modalMark, modalSel, modalEmpty, matchStyle lipgloss.Style
)

func init() {
	ts, _ := loadThemes(nil)
	t, _ := ts.get(defaultTheme, "")
	applyTheme(t)
}

func fg(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

func applyTheme(t Theme) {
	theme = t
	c := lipgloss.Color
	// The theme's background is the terminal's own while sltty runs (the
	// view sets it, see View), never painted cell by cell: the renderer
	// scrolls and erases lines assuming the terminal fills them with the
	// current background (bce), and on a painted background terminals that
	// do it differently were left with repeated rows (channels twice).
	paneBg, surfaceBg = "", bgSeq(t.Surface)
	sbBg, sbSelBg, sbSelFocusBg, selBg = bgSeq(t.SidebarBg), bgSeq(t.SidebarCursorBg), bgSeq(t.SidebarSelBg), bgSeq(t.Selection)
	if t.SidebarBg == t.Background {
		sbBg = "" // the same: the terminal's background as well
	}
	// A drag selection must stand apart from the persistent message cursor.
	dragSelBg = bgSeq(mix(cmpOr(t.Background, "#000000"), t.Accent, 0.42))
	if t.Background == "" {
		surfaceBg = "" // on the terminal's background, floating boxes stay on it too
	}

	sbTitle = fg(t.SidebarText).Bold(true)
	sbMuted = fg(t.SidebarMuted)
	sbBright = fg(t.SidebarText).Bold(true)
	sbSelText = fg(t.SidebarSelFg).Bold(true)
	sbOnline = fg(t.Accent)
	sbBadge = lipgloss.NewStyle().Background(c(t.Badge)).Foreground(c("#ffffff")).Bold(true).Padding(0, 1)
	sbBadgeMuted = lipgloss.NewStyle().Background(c(t.SidebarMuted)).Foreground(c(cmpOr(t.SidebarBg, "#000000"))).Padding(0, 1)
	// On the selected row, the red unread badge blends into the light selection.
	// Use the cursor color instead so its count remains clearly distinguishable.
	sbSelBadge = lipgloss.NewStyle().Background(c(t.SidebarCursorBg)).Foreground(c("#ffffff")).Bold(true).Padding(0, 1)
	sbMark = fg(t.Warning).Bold(true)
	sbDot = fg(t.Primary).Bold(true)

	headerStyle = fg(t.Text).Bold(true)
	headerFocus = fg(t.Text).Bold(true).Underline(true)
	nameStyle = fg(t.Username).Bold(true)
	meStyle = fg(t.Me).Bold(true)
	textStyle = fg(t.Text)
	dimStyle = fg(t.TextMuted)
	linkStyle = fg(t.Link).Bold(true)
	reactStyle = lipgloss.NewStyle().Background(c(t.ReactBg)).Foreground(c(t.Text)).Padding(0, 1)
	reactMine = lipgloss.NewStyle().Background(c(t.ReactMine)).Foreground(c(t.Text)).Padding(0, 1)
	dayStyle = fg(t.Text).Bold(true)
	ruleStyle = fg(t.Border)
	selMark = fg(t.Primary).Bold(true)
	openMark = fg(t.TextMuted)
	// Slack renders inline code as warm, muted chips rather than links.
	// Keep it distinct from the primary/link blue: bots use code for the
	// values that deserve attention (versions, percentages, hashes, etc.).
	keyStyle = lipgloss.NewStyle().Background(c(t.Surface)).Foreground(c(t.Warning))
	errStyle = fg(t.Error)
	editTitle = lipgloss.NewStyle().Background(c(t.Warning)).Foreground(c("#000000")).Bold(true)
	titleStyle = fg(t.Text).Bold(true)
	keyCap = lipgloss.NewStyle().Background(c(t.Border)).Foreground(c(t.Text)).Bold(true).Padding(0, 1)
	helpGroup = fg(t.Primary).Bold(true)

	normalPill = lipgloss.NewStyle().Background(c(t.Primary)).Foreground(c(cmpOr(t.Background, "#000000"))).Bold(true).Padding(0, 1)
	insertPill = lipgloss.NewStyle().Background(c(t.Accent)).Foreground(c("#000000")).Bold(true).Padding(0, 1)
	offlinePill = lipgloss.NewStyle().Background(c(t.Error)).Foreground(c("#ffffff")).Bold(true).Padding(0, 1)
	statusWhere = fg(t.Text).Bold(true)

	dividerStyle = fg(t.Border)
	dragStyle = fg(t.Warning)
	boxBorder = fg(t.Border)
	boxBorderFocus = fg(t.Primary)

	newRule = fg(t.Error)
	commandPill = lipgloss.NewStyle().Background(c(t.Warning)).Foreground(c("#000000")).Bold(true).Padding(0, 1)
	onDot = fg(t.Accent)
	spinStyle = fg(t.Accent)
	scopeChip = lipgloss.NewStyle().Background(c(t.Primary)).Foreground(c(cmpOr(t.Background, "#000000"))).Bold(true).Padding(0, 1)
	newStyle = fg(t.Error).Bold(true)

	modalBorder = fg(t.Primary)
	// A modal title is followed by a rule; keep it bright without competing
	// with the body or the keyboard-only footer.
	modalTitle = fg(t.Primary).Bold(true)
	modalInput = fg(t.Primary)
	modalMark = fg(t.Accent)
	modalSel = fg(t.Primary).Bold(true)
	modalEmpty = fg(t.TextMuted).Italic(true)
	matchStyle = fg(t.Warning).Bold(true)
}
