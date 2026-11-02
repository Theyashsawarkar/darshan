package index

import "testing"

func TestSearchRanksNamesFirst(t *testing.T) {
	paths := []string{"/home/a/report/notes.md", "/home/a/docs/report.pdf", "/home/a/reporter-photo.png", "/x/r/e/p/o/r/t.bin.pdf"}
	got := Search(paths, "report", "", nil, 10)
	if len(got) == 0 || got[0] != "/home/a/docs/report.pdf" {
		t.Fatalf("report: %v", got)
	}
	if got := Search(paths, "report", "image", nil, 10); len(got) != 1 || got[0] != "/home/a/reporter-photo.png" {
		t.Fatalf("kind: %v", got)
	}
	if got := Search(paths, "zzz", "", nil, 10); len(got) != 0 {
		t.Fatalf("no match: %v", got)
	}
}

func TestRecentFirstWhenEmpty(t *testing.T) {
	paths := []string{"/a.pdf", "/b.pdf", "/c.pdf"}
	got := Search(paths, "", "", []string{"/c.pdf"}, 10)
	if got[0] != "/c.pdf" {
		t.Fatalf("recent: %v", got)
	}
}
