package tui

import (
	"encoding/base64"
	"fmt"
	"image"
	"os"
	"os/exec"
	"strconv"
	"strings"

	"golang.org/x/sys/unix"

	"github.com/Theyashsawarkar/darshan/internal/doc"
)

// Pages and images show through kitty's graphics protocol: the image goes
// straight to the terminal (in tmux, to the client's own tty, so it works in
// popups too) and the screen draws Unicode placeholders where it belongs,
// which any text layout carries. Other terminals get coloured half blocks.
type gfx struct {
	kitty bool
	tty   string
	cw    int // a cell's size in pixels
	ch    int
}

func newGfx(client string) gfx {
	g := gfx{tty: "/dev/tty", cw: 10, ch: 20}
	term := os.Getenv("TERM")
	g.kitty = os.Getenv("KITTY_WINDOW_ID") != "" || term == "xterm-kitty" || os.Getenv("TERM_PROGRAM") == "ghostty"
	if os.Getenv("DARSHAN_GRAPHICS") == "blocks" {
		g.kitty = false
		return g
	}
	if os.Getenv("TMUX") != "" {
		args := []string{"display", "-p"}
		if client != "" {
			args = append(args, "-c", client)
		}
		out, err := exec.Command("tmux", append(args, "#{client_tty}\t#{client_termname}\t#{client_cell_width}\t#{client_cell_height}")...).Output()
		if err == nil {
			f := strings.Split(strings.TrimSpace(string(out)), "\t")
			if len(f) == 4 {
				if f[0] != "" {
					g.tty = f[0]
				}
				if strings.Contains(f[1], "kitty") || strings.Contains(f[1], "ghostty") {
					g.kitty = true
				}
				if w, _ := strconv.Atoi(f[2]); w > 0 {
					g.cw = w
				}
				if h, _ := strconv.Atoi(f[3]); h > 0 {
					g.ch = h
				}
			}
		}
		return g
	}
	if ws, err := unix.IoctlGetWinsize(int(os.Stdout.Fd()), unix.TIOCGWINSZ); err == nil && ws.Col > 0 && ws.Xpixel > 0 {
		g.cw, g.ch = int(ws.Xpixel)/int(ws.Col), int(ws.Ypixel)/int(ws.Row)
	}
	return g
}

func (g gfx) write(cmds ...string) {
	f, err := os.OpenFile(g.tty, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		return
	}
	defer f.Close()
	_, _ = f.WriteString(strings.Join(cmds, ""))
}

// place sends a PNG as image id, to fill cols × rows.
func (g gfx) place(id uint32, path string, cols, rows int) {
	g.write(fmt.Sprintf("\x1b_Ga=T,U=1,q=2,f=100,t=f,i=%d,c=%d,r=%d;%s\x1b\\", id, cols, rows, base64.StdEncoding.EncodeToString([]byte(path))))
}

func (g gfx) forget(ids ...uint32) {
	var cmds []string
	for _, id := range ids {
		cmds = append(cmds, fmt.Sprintf("\x1b_Ga=d,d=I,i=%d,q=2\x1b\\", id))
	}
	g.write(cmds...)
}

// placeholders are the cells kitty fills with image id: U+10EEEE, its row
// and column as diacritics, the id as the foreground colour.
func placeholders(id uint32, cols, rows int) []string {
	fg := fmt.Sprintf("\x1b[38;2;%d;%d;%dm", id>>16&255, id>>8&255, id&255)
	out := make([]string, rows)
	for r := 0; r < rows && r < len(diacritics); r++ {
		var b strings.Builder
		b.WriteString(fg)
		for c := 0; c < cols && c < len(diacritics); c++ {
			b.WriteRune(0x10EEEE)
			b.WriteRune(diacritics[r])
			b.WriteRune(diacritics[c])
		}
		b.WriteString("\x1b[39m")
		out[r] = b.String()
	}
	return out
}

// halfBlocks draws an image with ▀: two pixels a cell, true colour.
func halfBlocks(img image.Image, cols, rows int) []string {
	small := doc.Scale(img, cols, rows*2).(*image.RGBA)
	out := make([]string, rows)
	for r := range rows {
		var b strings.Builder
		for c := range cols {
			t, u := small.RGBAAt(c, 2*r), small.RGBAAt(c, 2*r+1)
			fmt.Fprintf(&b, "\x1b[38;2;%d;%d;%dm\x1b[48;2;%d;%d;%dm▀", t.R, t.G, t.B, u.R, u.G, u.B)
		}
		b.WriteString("\x1b[0m")
		out[r] = b.String()
	}
	return out
}
