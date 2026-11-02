// Package ui draws the screen's pieces the way the other Vayu terminal
// tools do (sandesh, sangam): rounded boxes with the title in the border,
// a tab row, a footer of round key caps, popups drawn over the screen.
// Colours follow varna's desktop theme (~/.local/state/varna/palette.json)
// and change live when it does; without varna they are Catppuccin Mocha.
package ui

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

// Theme is the colours the screen uses, as #rrggbb.
type Theme struct {
	Accent, Accent2, Text, Sub, Muted, Surface, Border, Base string
	Green, Yellow, Red, Blue, Peach, Teal, Sky               string
}

// Mocha is Catppuccin Mocha, the colours without varna.
var Mocha = Theme{
	Accent: "#cba6f7", Accent2: "#f5c2e7", Text: "#cdd6f4", Sub: "#a6adc8", Muted: "#7f849c",
	Surface: "#313244", Border: "#45475a", Base: "#1e1e2e",
	Green: "#a6e3a1", Yellow: "#f9e2af", Red: "#f38ba8", Blue: "#89b4fa", Peach: "#fab387", Teal: "#94e2d5", Sky: "#89dceb",
}

// Palette is the file varna writes.
func Palette() string {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	return filepath.Join(dir, "varna", "palette.json")
}

// LoadTheme is Mocha with varna's colours over it: the theme's pick for
// this tool under tools.<tool> if it has one, else the desktop's.
func LoadTheme(tool string) Theme {
	t := Mocha
	b, err := os.ReadFile(Palette())
	if err != nil {
		return t
	}
	type theme struct {
		Colors map[string]any `json:"colors"`
	}
	var data struct {
		theme
		Tools map[string]theme `json:"tools"`
	}
	if json.Unmarshal(b, &data) != nil {
		return t
	}
	cols := data.Colors
	if tt, ok := data.Tools[tool]; ok && len(tt.Colors) > 0 {
		cols = tt.Colors
	}
	pick := func(dst *string, names ...string) {
		for _, n := range names {
			if v, ok := cols[n].(string); ok && strings.HasPrefix(v, "#") && len(v) >= 7 {
				*dst = v[:7]
				return
			}
		}
	}
	pick(&t.Accent, "accent")
	pick(&t.Accent2, "accent2", "pink")
	pick(&t.Text, "text", "fg")
	pick(&t.Sub, "subtext0", "subtext1")
	pick(&t.Muted, "overlay1", "muted")
	pick(&t.Surface, "surface0", "selection")
	pick(&t.Border, "surface1", "surface2", "line")
	pick(&t.Base, "base", "bg")
	pick(&t.Green, "green", "ok")
	pick(&t.Yellow, "yellow", "warn")
	pick(&t.Red, "red", "error")
	pick(&t.Blue, "blue", "info")
	pick(&t.Peach, "peach", "maroon")
	pick(&t.Teal, "teal", "green")
	pick(&t.Sky, "sky", "blue")
	return t
}

// ThemeStamp changes when varna writes a new palette.
func ThemeStamp() string {
	if st, err := os.Stat(Palette()); err == nil {
		return st.ModTime().Format(time.RFC3339Nano)
	}
	return ""
}

// Styles are the theme's colours ready to draw with.
type Styles struct {
	T                                          Theme
	Text, Sub, Muted, Accent, Bold             lipgloss.Style
	Green, Yellow, Red, Blue, Peach, Teal, Sky lipgloss.Style
	Nerd                                       bool // Nerd Font glyphs (round caps, icons)
}

// New makes the styles for a theme.
func New(t Theme) Styles {
	return Styles{
		T: t, Text: FG(t.Text), Sub: FG(t.Sub), Muted: FG(t.Muted), Accent: FG(t.Accent), Bold: FG(t.Text).Bold(true),
		Green: FG(t.Green), Yellow: FG(t.Yellow), Red: FG(t.Red), Blue: FG(t.Blue), Peach: FG(t.Peach), Teal: FG(t.Teal), Sky: FG(t.Sky),
		Nerd: os.Getenv("NO_NERD_FONT") == "",
	}
}

// FG is text in a colour.
func FG(c string) lipgloss.Style { return lipgloss.NewStyle().Foreground(lipgloss.Color(c)) }

// Pill is bold dark text on a colour, with round ends.
func (s Styles) Pill(text, bg string) string {
	return s.Round(lipgloss.NewStyle().Foreground(lipgloss.Color(s.T.Base)).Background(lipgloss.Color(bg)).Bold(true), " "+text+" ")
}

// Chip is coloured text on the surface colour, with round ends.
func (s Styles) Chip(text, fg string) string {
	return s.Round(lipgloss.NewStyle().Foreground(lipgloss.Color(fg)).Background(lipgloss.Color(s.T.Surface)), " "+text+" ")
}

// Key is a key cap: the key, round, then what it does.
func (s Styles) Key(k, what string) string {
	cap := s.Round(lipgloss.NewStyle().Foreground(lipgloss.Color(s.T.Text)).Background(lipgloss.Color(s.T.Surface)), " "+k+" ")
	if what == "" {
		return cap
	}
	return cap + " " + s.Muted.Render(what)
}

// Round draws a filled label with Nerd Font half circles for ends.
func (s Styles) Round(st lipgloss.Style, text string) string {
	if !s.Nerd {
		return st.Render(text)
	}
	cap := lipgloss.NewStyle().Foreground(st.GetBackground())
	return cap.Render("") + st.Render(strings.TrimSuffix(strings.TrimPrefix(text, " "), " ")) + cap.Render("")
}

// Fit cuts or pads s to exactly w cells.
func Fit(s string, w int) string {
	if w <= 0 {
		return ""
	}
	s = ansi.Truncate(s, w, "…")
	return s + strings.Repeat(" ", max(0, w-lipgloss.Width(s)))
}

// Row puts left and right at the two ends of w cells.
func Row(left, right string, w int) string {
	gap := w - lipgloss.Width(left) - lipgloss.Width(right)
	if gap < 1 {
		left = ansi.Truncate(left, max(1, w-lipgloss.Width(right)-1), "…")
		gap = max(1, w-lipgloss.Width(left)-lipgloss.Width(right))
	}
	return left + strings.Repeat(" ", gap) + right
}

// Box is a rounded box w×h (borders included) with the title in its top
// border, accent-coloured when focused.
func (s Styles) Box(w, h int, title string, lines []string, focus bool, color string) string {
	bc, ts := s.T.Border, s.Muted
	if focus {
		if color == "" {
			color = s.T.Accent
		}
		bc, ts = color, FG(color).Bold(true)
	}
	b := FG(bc)
	inner := w - 2
	t := ansi.Truncate(" "+title+" ", max(0, inner-2), "…")
	out := []string{b.Render("╭─") + ts.Render(t) + b.Render(strings.Repeat("─", max(0, inner-1-lipgloss.Width(t)))+"╮")}
	for i := 0; i < h-2; i++ {
		l := ""
		if i < len(lines) {
			l = lines[i]
		}
		out = append(out, b.Render("│")+" "+Fit(l, inner-2)+" "+b.Render("│"))
	}
	out = append(out, b.Render("╰"+strings.Repeat("─", inner)+"╯"))
	return strings.Join(out, "\n")
}

// Popup is a box sized to its lines, for drawing over the screen.
func (s Styles) Popup(title string, lines []string, w int, color string) []string {
	if color == "" {
		color = s.T.Accent
	}
	return strings.Split(s.Box(w, len(lines)+2, title, lines, true, color), "\n")
}

// Overlay draws box over base with its top at row y, centred across.
func Overlay(base string, box []string, width, y int) string {
	lines := strings.Split(base, "\n")
	bw := 0
	for _, l := range box {
		bw = max(bw, lipgloss.Width(l))
	}
	x := max(0, (width-bw)/2)
	for i, l := range box {
		r := y + i
		if r < 0 || r >= len(lines) {
			continue
		}
		left := ansi.Cut(lines[r], 0, x)
		left += strings.Repeat(" ", max(0, x-lipgloss.Width(left)))
		lines[r] = left + l + ansi.Cut(lines[r], x+lipgloss.Width(l), width)
	}
	return strings.Join(lines, "\n")
}

// Tabs is the top row: the tabs, the current one a filled pill, and a
// note on the right.
func (s Styles) Tabs(tabs []string, cur int, right string, w int) string {
	var b strings.Builder
	b.WriteString(" ")
	for i, t := range tabs {
		if i == cur {
			b.WriteString(s.Pill(t, s.T.Accent))
		} else {
			b.WriteString(s.Muted.Render(" " + t + " "))
		}
		b.WriteString(" ")
	}
	return Row(b.String(), s.Muted.Render(right)+" ", w)
}

// Footer is the bottom row: the mode pill, then key caps, as many as fit.
func (s Styles) Footer(mode string, keys [][2]string, msg string, w int) string {
	out := " " + s.Pill(mode, s.T.Accent)
	if msg != "" {
		return Fit(out+"  "+msg, w)
	}
	for _, k := range keys {
		next := "  " + s.Key(k[0], k[1])
		if lipgloss.Width(out+next) > w-1 {
			break
		}
		out += next
	}
	return Fit(out, w)
}

// Column is a table column: its heading and width (0: takes what's left).
type Column struct {
	Title string
	Width int
}

// Table lays out rows under headings in w cells; the selected row gets
// an accent bar on the left. top is the first row shown, h the rows that
// fit (heading included).
func (s Styles) Table(cols []Column, rows [][]string, sel, top, w, h int) []string {
	widths := make([]int, len(cols))
	used, flex := 2, -1
	for i, c := range cols {
		widths[i] = c.Width
		if c.Width == 0 {
			flex = i
		}
		used += c.Width + 1
	}
	if flex >= 0 {
		widths[flex] = max(4, w-used)
	}
	line := func(cells []string, head bool) string {
		var b strings.Builder
		for i, c := range cells {
			if i >= len(widths) {
				break
			}
			if head {
				c = s.Muted.Render(strings.ToUpper(c))
			}
			b.WriteString(Fit(c, widths[i]))
			if i < len(widths)-1 {
				b.WriteString(" ")
			}
		}
		return b.String()
	}
	heads := make([]string, len(cols))
	for i, c := range cols {
		heads[i] = c.Title
	}
	out := []string{"  " + line(heads, true)}
	for i := top; i < len(rows) && len(out) < h; i++ {
		if i == sel {
			out = append(out, s.Accent.Render("▌ ")+s.Highlight(line(rows[i], false), w-2))
			continue
		}
		out = append(out, "  "+line(rows[i], false))
	}
	return out
}

// Highlight paints a line's background in the surface colour, through
// the colour resets inside it.
func (s Styles) Highlight(line string, w int) string {
	bg := bgSeq(s.T.Surface)
	line = Fit(line, w)
	return bg + strings.ReplaceAll(line, "\x1b[0m", "\x1b[0m"+bg) + "\x1b[0m"
}

func bgSeq(hex string) string {
	var r, g, b int
	if _, err := fmt.Sscanf(hex, "#%02x%02x%02x", &r, &g, &b); err != nil {
		return ""
	}
	return fmt.Sprintf("\x1b[48;2;%d;%d;%dm", r, g, b)
}

// Scroll keeps sel inside a window of h rows starting at top.
func Scroll(sel, top, h int) int {
	if h < 1 {
		return 0
	}
	if sel < top {
		return sel
	}
	if sel >= top+h {
		return sel - h + 1
	}
	return top
}

// KV is "key   value" lines with the keys in a column.
func (s Styles) KV(pairs [][2]string, kw int) []string {
	var out []string
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		out = append(out, s.Muted.Render(Fit(p[0], kw))+" "+p[1])
	}
	return out
}

// Heading is a small section title inside a pane.
func (s Styles) Heading(t string) string { return s.Muted.Render(strings.ToUpper(t)) }

// Wrap breaks plain text into lines of at most w cells.
func Wrap(t string, w int) []string {
	var out []string
	for _, para := range strings.Split(t, "\n") {
		line := ""
		for _, word := range strings.Fields(para) {
			if line != "" && lipgloss.Width(line)+1+lipgloss.Width(word) > w {
				out = append(out, line)
				line = ""
			}
			if line != "" {
				line += " "
			}
			line += word
		}
		out = append(out, line)
	}
	return out
}
