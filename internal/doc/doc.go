// Package doc reads what darshan shows: PDFs (through poppler's tools),
// images and Markdown.
package doc

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"

	"github.com/charmbracelet/glamour"
	_ "golang.org/x/image/bmp"
	xdraw "golang.org/x/image/draw"
	_ "golang.org/x/image/webp"
)

// Kinds of file.
const (
	PDF      = "pdf"
	Image    = "image"
	Markdown = "markdown"
)

// Exts maps every extension darshan opens to its kind.
var Exts = map[string]string{
	".pdf": PDF, ".png": Image, ".jpg": Image, ".jpeg": Image, ".gif": Image, ".webp": Image, ".bmp": Image,
	".md": Markdown, ".markdown": Markdown,
}

// KindOf says what a file is ("" when darshan can't show it).
func KindOf(path string) string { return Exts[strings.ToLower(filepath.Ext(path))] }

// Stamp changes when a file does.
func Stamp(path string) string {
	fi, err := os.Stat(path)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%d-%d", fi.Size(), fi.ModTime().UnixNano())
}

// Size is a page's or an image's size, in points or pixels.
type Size struct{ W, H float64 }

var (
	rePages = regexp.MustCompile(`(?m)^Pages:\s+(\d+)`)
	reSize  = regexp.MustCompile(`(?m)^Page\s+\d+\s+size:\s+([\d.]+) x ([\d.]+)`)
	reRot   = regexp.MustCompile(`(?m)^Page\s+\d+\s+rot:\s+(\d+)`)
)

// PDFPages counts a PDF's pages.
func PDFPages(path string) (int, error) {
	out, err := exec.Command("pdfinfo", path).CombinedOutput()
	if err != nil {
		if errors.Is(err, exec.ErrNotFound) {
			return 0, errors.New("showing PDFs needs poppler (pdfinfo, pdftoppm, pdftotext)")
		}
		return 0, fmt.Errorf("pdfinfo: %s", firstLine(string(out)))
	}
	m := rePages.FindSubmatch(out)
	if m == nil {
		return 0, errors.New("pdfinfo: no page count")
	}
	return strconv.Atoi(string(m[1]))
}

// PDFPageSize is a page's size in points, rotation applied.
func PDFPageSize(path string, page int) (Size, error) {
	p := strconv.Itoa(page)
	out, err := exec.Command("pdfinfo", "-f", p, "-l", p, path).Output()
	if err != nil {
		return Size{}, err
	}
	m := reSize.FindSubmatch(out)
	if m == nil {
		return Size{}, errors.New("pdfinfo: no page size")
	}
	w, _ := strconv.ParseFloat(string(m[1]), 64)
	h, _ := strconv.ParseFloat(string(m[2]), 64)
	if r := reRot.FindSubmatch(out); r != nil {
		if d, _ := strconv.Atoi(string(r[1])); d%180 != 0 {
			w, h = h, w
		}
	}
	return Size{w, h}, nil
}

// RenderPDF returns a page as an image pxW wide, cached on disk.
func RenderPDF(cache, path string, page, pxW int) (image.Image, error) {
	h := sha256.Sum256([]byte(path + "|" + Stamp(path)))
	dir := filepath.Join(cache, "pages")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	base := filepath.Join(dir, fmt.Sprintf("%s-%d-%d", hex.EncodeToString(h[:10]), page, pxW))
	if _, err := os.Stat(base + ".png"); err != nil {
		p := strconv.Itoa(page)
		out, err := exec.Command("pdftoppm", "-png", "-singlefile", "-f", p, "-l", p,
			"-scale-to-x", strconv.Itoa(pxW), "-scale-to-y", "-1", path, base).CombinedOutput()
		if err != nil {
			return nil, fmt.Errorf("pdftoppm: %s", firstLine(string(out)))
		}
	}
	return decodeFile(base + ".png")
}

// PDFText is the text of every page.
func PDFText(path string) ([]string, error) {
	out, err := exec.Command("pdftotext", "-layout", path, "-").Output()
	if err != nil {
		return nil, err
	}
	pages := strings.Split(string(out), "\f")
	if n := len(pages); n > 0 && strings.TrimSpace(pages[n-1]) == "" {
		pages = pages[:n-1]
	}
	return pages, nil
}

func decodeFile(path string) (image.Image, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	img, _, err := image.Decode(bytes.NewReader(b))
	return img, err
}

// LoadImage decodes an image file.
func LoadImage(path string) (image.Image, error) {
	img, err := decodeFile(path)
	if err != nil {
		return nil, fmt.Errorf("%s: %w", filepath.Base(path), err)
	}
	return img, nil
}

// Scale resizes an image to w × h.
func Scale(src image.Image, w, h int) image.Image {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	xdraw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), xdraw.Over, nil)
	return dst
}

// Crop cuts w × h out of src at x, y, on white or (inverted) black; invert
// flips light and dark, which reads better at night.
func Crop(src image.Image, x, y, w, h int, invert bool) *image.RGBA {
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), image.NewUniform(color.White), image.Point{}, draw.Src)
	draw.Draw(dst, dst.Bounds(), src, src.Bounds().Min.Add(image.Pt(x, y)), draw.Over)
	if invert {
		for i := 0; i+3 < len(dst.Pix); i += 4 {
			dst.Pix[i], dst.Pix[i+1], dst.Pix[i+2] = 255-dst.Pix[i], 255-dst.Pix[i+1], 255-dst.Pix[i+2]
		}
	}
	return dst
}

// WritePNG saves an image.
func WritePNG(path string, img image.Image) error {
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	enc := png.Encoder{CompressionLevel: png.BestSpeed}
	return enc.Encode(f, img)
}

// RenderMarkdown draws Markdown as styled terminal lines.
func RenderMarkdown(path string, width int) ([]string, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	r, err := glamour.NewTermRenderer(glamour.WithStandardStyle("dark"), glamour.WithWordWrap(width))
	if err != nil {
		return nil, err
	}
	out, err := r.Render(string(b))
	if err != nil {
		return nil, err
	}
	return strings.Split(strings.TrimRight(out, "\n"), "\n"), nil
}

func firstLine(s string) string {
	s, _, _ = strings.Cut(strings.TrimSpace(s), "\n")
	return s
}
