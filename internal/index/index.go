// Package index finds every PDF, image and Markdown file on the system:
// a list kept on disk so search is instant, refreshed in the background.
package index

import (
	"bufio"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"

	"github.com/BurntSushi/toml"

	"github.com/Theyashsawarkar/darshan/internal/doc"
	"github.com/Theyashsawarkar/darshan/internal/state"
)

// Config is config.toml's say on where to look.
type Config struct {
	Roots   []string `toml:"roots"`   // added to the default folders
	Exclude []string `toml:"exclude"` // added to the default skips
}

// LoadConfig reads config.toml.
func LoadConfig() Config {
	var c Config
	_, _ = toml.DecodeFile(filepath.Join(state.ConfigDir(), "config.toml"), &c)
	return c
}

var skip = []string{"node_modules", ".git", ".cache", ".cargo", ".rustup", ".npm", ".pnpm-store", ".pnpm", "Trash", "site-packages",
	".venv", "venv", "__pycache__", ".gradle", ".android", ".m2", "pkg/mod", ".mozilla", ".zen", ".config/google-chrome", "darshan/pages"}

// Roots are the folders searched: home, other disks and system documents.
func Roots(c Config) []string {
	if only := os.Getenv("DARSHAN_ROOTS"); only != "" { // tests and demos: only here
		return strings.Split(only, ":")
	}
	h, _ := os.UserHomeDir()
	var out []string
	for _, r := range append([]string{h, "/mnt", "/media", "/run/media", "/opt", "/srv", "/usr/share/doc", os.TempDir()}, c.Roots...) {
		if strings.HasPrefix(r, "~/") {
			r = filepath.Join(h, r[2:])
		}
		if fi, err := os.Stat(r); err == nil && fi.IsDir() {
			out = append(out, r)
		}
	}
	return out
}

func cachePath() string { return filepath.Join(state.CacheDir(), "index") }

// Cached is the list from the last scan.
func Cached() []string {
	b, err := os.ReadFile(cachePath())
	if err != nil {
		return nil
	}
	return strings.Split(strings.TrimSpace(string(b)), "\n")
}

// Refresh scans the disks with fd, reports the paths in batches, and saves
// the list.
func Refresh(ctx context.Context, c Config, batch func([]string)) ([]string, error) {
	args := []string{"--type", "f", "--absolute-path", "--hidden", "--no-ignore", "--color", "never"}
	for ext := range doc.Exts {
		args = append(args, "-e", strings.TrimPrefix(ext, "."))
	}
	for _, s := range append(append([]string{}, skip...), c.Exclude...) {
		args = append(args, "-E", s)
	}
	args = append(args, ".")
	args = append(args, Roots(c)...)
	bin := "fd"
	if _, err := exec.LookPath(bin); err != nil {
		bin = "fdfind" // Debian's name
	}
	cmd := exec.CommandContext(ctx, bin, args...)
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	var all, cur []string
	sc := bufio.NewScanner(out)
	sc.Buffer(make([]byte, 1<<16), 1<<20)
	for sc.Scan() {
		all = append(all, sc.Text())
		if cur = append(cur, sc.Text()); len(cur) >= 2000 {
			if batch != nil {
				batch(cur)
			}
			cur = nil
		}
	}
	if batch != nil && len(cur) > 0 {
		batch(cur)
	}
	_ = cmd.Wait() // fd exits non-zero over a folder it can't read
	if ctx.Err() != nil {
		return all, ctx.Err()
	}
	if err := os.MkdirAll(state.CacheDir(), 0o700); err == nil {
		_ = os.WriteFile(cachePath(), []byte(strings.Join(all, "\n")+"\n"), 0o600)
	}
	return all, nil
}

// score ranks s for query q (lower-case): -1 when q isn't in it in order.
// Matches in the file name, at word starts and in a row count most.
func score(q, path string) int {
	s := strings.ToLower(path)
	base := strings.LastIndexByte(s, '/') + 1
	i, total, run := 0, 0, 0
	for _, r := range q {
		j := strings.IndexRune(s[i:], r)
		if j < 0 {
			return -1
		}
		at := i + j
		if j == 0 && i > 0 {
			run++
			total += 6 * run
		} else {
			run = 0
		}
		if at >= base {
			total += 8
		}
		if at == 0 || strings.ContainsRune("/ -_.", rune(s[at-1])) {
			total += 10
		}
		i = at + len(string(r))
	}
	if k := strings.Index(s[base:], q); k >= 0 {
		total += 60 + 4*len(q)
		if k == 0 {
			total += 30
		}
	}
	return total - len(s)/16
}

// Search ranks paths for a query; kind "" is every kind. Recent files come
// first when the query is empty and rank higher otherwise.
func Search(paths []string, q, kind string, recent []string, limit int) []string {
	q = strings.ToLower(strings.Join(strings.Fields(q), ""))
	boost := map[string]int{}
	for i, p := range recent {
		boost[p] = 40 - min(i, 39)
	}
	type hit struct {
		p string
		s int
	}
	var hits []hit
	seen := map[string]bool{}
	for _, p := range append(append([]string{}, recent...), paths...) {
		if seen[p] || p == "" || (kind != "" && doc.KindOf(p) != kind) {
			continue
		}
		seen[p] = true
		s := score(q, p)
		if s < 0 && q != "" {
			continue
		}
		hits = append(hits, hit{p, s + boost[p]*3})
	}
	if q == "" {
		// recent first, in order; then the rest by name
		sort.SliceStable(hits, func(i, j int) bool {
			if a, b := boost[hits[i].p], boost[hits[j].p]; a != b {
				return a > b
			}
			return hits[i].p < hits[j].p
		})
	} else {
		sort.SliceStable(hits, func(i, j int) bool { return hits[i].s > hits[j].s })
	}
	if len(hits) > limit {
		hits = hits[:limit]
	}
	out := make([]string, len(hits))
	for i, h := range hits {
		out[i] = h.p
	}
	return out
}
