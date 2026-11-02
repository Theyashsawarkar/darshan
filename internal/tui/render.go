package tui

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"sync"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/Theyashsawarkar/darshan/internal/doc"
	"github.com/Theyashsawarkar/darshan/internal/state"
)

// job asks for one view of a page or image.
type job struct {
	Path, Kind string
	Page       int
	Cols, Rows int // the room, in cells
	CW, CH     int
	Zoom       float64 // 1 fits
	FitWidth   bool
	PanX, PanY int
	Invert     bool
	Gen        int
	Preview    bool
	Kitty      bool
}

// shot is the result: a PNG for kitty or blocks for the rest.
type shot struct {
	job
	PNG        string
	Blocks     []string
	ViewCols   int
	ViewRows   int
	TargetW    int
	TargetH    int
	PanX, PanY int
	Err        error
}

type shotMsg shot

// Decoded images stay in memory, so panning a zoomed page doesn't reload it.
var (
	srcMu  sync.Mutex
	srcKey string
	srcImg image.Image
)

func source(j job, targetW int) (image.Image, error) {
	key := fmt.Sprint(j.Path, doc.Stamp(j.Path), j.Page, targetW)
	if j.Kind == doc.Image {
		key = fmt.Sprint(j.Path, doc.Stamp(j.Path))
	}
	srcMu.Lock()
	if key == srcKey && srcImg != nil {
		defer srcMu.Unlock()
		return srcImg, nil
	}
	srcMu.Unlock()
	var img image.Image
	var err error
	if j.Kind == doc.PDF {
		img, err = doc.RenderPDF(state.CacheDir(), j.Path, j.Page, targetW)
	} else {
		img, err = doc.LoadImage(j.Path)
	}
	if err != nil {
		return nil, err
	}
	srcMu.Lock()
	srcKey, srcImg = key, img
	srcMu.Unlock()
	return img, nil
}

// PageSize is a page's or image's natural size.
func pageSize(j job) (doc.Size, error) {
	if j.Kind == doc.PDF {
		return doc.PDFPageSize(j.Path, j.Page)
	}
	f, err := os.Open(j.Path)
	if err != nil {
		return doc.Size{}, err
	}
	defer f.Close()
	c, _, err := image.DecodeConfig(f)
	if err != nil {
		return doc.Size{}, fmt.Errorf("%s: %w", filepath.Base(j.Path), err)
	}
	return doc.Size{W: float64(c.Width), H: float64(c.Height)}, nil
}

// take renders the job: fits the page in the room, applies the zoom, cuts
// out what shows.
func take(j job) shot {
	r := shot{job: j, PanX: j.PanX, PanY: j.PanY}
	size, err := pageSize(j)
	if err != nil || size.W <= 0 || size.H <= 0 {
		r.Err = err
		return r
	}
	roomW, roomH := float64(j.Cols*j.CW), float64(j.Rows*j.CH)
	scale := math.Min(roomW/size.W, roomH/size.H)
	if j.FitWidth {
		scale = roomW / size.W
	}
	if j.Zoom > 0 {
		scale *= j.Zoom
	}
	if j.Kind == doc.Image && j.Zoom <= 1 && !j.FitWidth && scale > 1 && size.W < roomW/2 && size.H < roomH/2 {
		scale = max(1, scale/2) // small pictures don't blow up to the whole screen
	}
	tw := min(int(size.W*scale), 8000)
	th := int(float64(tw) * size.H / size.W)
	r.ViewCols = max(1, min(tw, j.Cols*j.CW)/j.CW)
	r.ViewRows = max(1, min(th, j.Rows*j.CH)/j.CH)
	vw, vh := r.ViewCols*j.CW, r.ViewRows*j.CH
	if tw < j.Cols*j.CW && th < j.Rows*j.CH || !j.FitWidth && j.Zoom <= 1 {
		// all of it shows: exactly as many pixels as the cells it takes
		tw, th = vw, vh
	}
	r.TargetW, r.TargetH = tw, th
	r.PanX = max(0, min(j.PanX, tw-vw))
	r.PanY = max(0, min(j.PanY, th-vh))
	img, err := source(j, tw)
	if err != nil {
		r.Err = err
		return r
	}
	if b := img.Bounds(); j.Kind == doc.Image && (b.Dx() != tw || b.Dy() != th) {
		img = doc.Scale(img, tw, th)
	}
	view := doc.Crop(img, r.PanX, r.PanY, vw, vh, j.Invert)
	if !j.Kittyish() {
		r.Blocks = halfBlocks(view, r.ViewCols, r.ViewRows)
		return r
	}
	dir := filepath.Join(state.CacheDir(), "view")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		r.Err = err
		return r
	}
	r.PNG = filepath.Join(dir, fmt.Sprintf("%d-%d-%v.png", os.Getpid(), j.Gen, j.Preview))
	r.Err = doc.WritePNG(r.PNG, view)
	return r
}

// Kittyish says whether the terminal takes real images.
func (j job) Kittyish() bool { return j.Kitty }

func renderCmd(j job) tea.Cmd {
	return func() tea.Msg { return shotMsg(take(j)) }
}

// prerender readies the next PDF page so turning it is instant.
func prerender(j job) tea.Cmd {
	return func() tea.Msg {
		size, err := doc.PDFPageSize(j.Path, j.Page)
		if err == nil && size.W > 0 {
			scale := math.Min(float64(j.Cols*j.CW)/size.W, float64(j.Rows*j.CH)/size.H)
			if j.FitWidth {
				scale = float64(j.Cols*j.CW) / size.W
			}
			tw := min(int(size.W*scale*max(j.Zoom, 1)), 8000)
			if !j.FitWidth && j.Zoom <= 1 {
				tw = max(1, tw/j.CW) * j.CW
			}
			_, _ = doc.RenderPDF(state.CacheDir(), j.Path, j.Page, tw)
		}
		return nil
	}
}
