package library

import (
	"os"
	"path/filepath"
	"testing"
)

// tree builds a real directory: mocking a filesystem would only test the mock,
// and every rule here is about what a real one contains.
func tree(t *testing.T, files ...string) string {
	t.Helper()
	dir := t.TempDir()
	for _, name := range files {
		path := filepath.Join(dir, name)
		if filepath.Ext(name) == "" {
			if err := os.MkdirAll(path, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(path, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func names(l Listing) []string {
	out := make([]string, 0, len(l.Entries))
	for _, e := range l.Entries {
		if e.IsDir {
			out = append(out, e.Name+"/")
			continue
		}
		out = append(out, e.Name+e.Ext)
	}
	return out
}

func TestScanPutsDirectoriesFirstThenSortsByName(t *testing.T) {
	dir := tree(t, "zebra", "beta.gp", "Alpha.mid", "apple")
	l, err := Scan(dir)
	if err != nil {
		t.Fatal(err)
	}

	got := names(l)
	want := []string{"apple/", "zebra/", "Alpha.mid", "beta.gp"}
	if len(got) != len(want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("got %v, want %v", got, want)
		}
	}
}

func TestScanSkipsHiddenEntries(t *testing.T) {
	// A browser that opens on a home directory and lists forty dotfiles is one
	// nobody uses twice.
	dir := tree(t, ".hidden.gp", ".config", "visible.gp")
	l, _ := Scan(dir)
	if got := names(l); len(got) != 1 || got[0] != "visible.gp" {
		t.Fatalf("got %v", got)
	}
}

func TestScanSkipsWhatCannotBeOpened(t *testing.T) {
	dir := tree(t, "notes.txt", "song.gp", "picture.png", "backing.wav")
	l, _ := Scan(dir)
	if got := names(l); len(got) != 2 {
		t.Fatalf("got %v, want only the score and the backing track", got)
	}
}

func TestScanFiltersByKind(t *testing.T) {
	dir := tree(t, "song.gp", "song.musicxml", "backing.wav", "backing.flac")

	scores, _ := Scan(dir, Score)
	if got := names(scores); len(got) != 2 {
		t.Fatalf("scores: %v", got)
	}
	backings, _ := Scan(dir, Backing)
	if got := names(backings); len(got) != 2 {
		t.Fatalf("backing tracks: %v", got)
	}
	both, _ := Scan(dir, Score, Backing)
	if got := names(both); len(got) != 4 {
		t.Fatalf("both: %v", got)
	}
}

func TestScanReportsTheDirectoryAbove(t *testing.T) {
	dir := tree(t, "inner/song.gp")
	l, err := Scan(filepath.Join(dir, "inner"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Parent != dir {
		t.Fatalf("parent %q, want %q", l.Parent, dir)
	}
}

func TestScanOnAFileScansItsParent(t *testing.T) {
	// A path pasted from somewhere else is as likely to be a file as a folder.
	dir := tree(t, "song.gp")
	l, err := Scan(filepath.Join(dir, "song.gp"))
	if err != nil {
		t.Fatal(err)
	}
	if l.Dir != dir {
		t.Fatalf("scanned %q, want %q", l.Dir, dir)
	}
	if got := names(l); len(got) != 1 {
		t.Fatalf("got %v", got)
	}
}

func TestScanOfAnEmptyDirectoryIsNotAnError(t *testing.T) {
	l, err := Scan(t.TempDir())
	if err != nil {
		t.Fatalf("empty directory: %v", err)
	}
	if len(l.Entries) != 0 {
		t.Fatalf("got %v", names(l))
	}
}

func TestScanOfSomethingThatIsNotThere(t *testing.T) {
	if _, err := Scan(filepath.Join(t.TempDir(), "nowhere")); err == nil {
		t.Fatal("expected an error")
	}
}

func TestScanFollowsASymlinkedDirectoryAndSurvivesABrokenOne(t *testing.T) {
	dir := tree(t, "real/song.gp")
	if err := os.Symlink(filepath.Join(dir, "real"), filepath.Join(dir, "link")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}
	if err := os.Symlink(filepath.Join(dir, "gone"), filepath.Join(dir, "broken.gp")); err != nil {
		t.Skipf("no symlinks here: %v", err)
	}

	l, err := Scan(dir)
	if err != nil {
		t.Fatalf("a broken link stopped the scan: %v", err)
	}
	var sawLink bool
	for _, e := range l.Entries {
		if e.Name == "link" && e.IsDir {
			sawLink = true
		}
		if e.Name == "broken" {
			t.Fatal("a broken link was listed as openable")
		}
	}
	if !sawLink {
		t.Fatalf("the symlinked directory was not listed: %v", names(l))
	}
}

func TestKindOf(t *testing.T) {
	cases := []struct {
		path string
		kind Kind
		ok   bool
	}{
		{"a.gp", Score, true},
		{"a.GP", Score, true},
		{"a.musicxml", Score, true},
		{"a.mxl", Score, true},
		{"a.mid", Score, true},
		{"a.wav", Backing, true},
		{"a.WAV", Backing, true},
		{"a.mp3", Backing, true},
		{"a.flac", Backing, true},
		{"a.txt", Score, false},
		{"a", Score, false},
		{"", Score, false},
	}
	for _, c := range cases {
		kind, ok := KindOf(c.path)
		if ok != c.ok || (ok && kind != c.kind) {
			t.Fatalf("%q = %v %v, want %v %v", c.path, kind, ok, c.kind, c.ok)
		}
	}
}

func TestPlacesHonoursXDGAndDoesNotRepeatItself(t *testing.T) {
	music := t.TempDir()
	t.Setenv("XDG_MUSIC_DIR", music)

	var found bool
	seen := map[string]bool{}
	for _, p := range Places() {
		if seen[p.Path] {
			t.Fatalf("%q listed twice", p.Path)
		}
		seen[p.Path] = true
		if p.Path == music {
			found = true
		}
		if !p.IsDir {
			t.Fatalf("%q is not marked as a directory", p.Name)
		}
	}
	if !found {
		t.Fatal("XDG_MUSIC_DIR was not offered")
	}
}

func TestPlacesLeavesOutWhatIsNotThere(t *testing.T) {
	t.Setenv("XDG_MUSIC_DIR", filepath.Join(t.TempDir(), "nowhere"))
	for _, p := range Places() {
		if _, err := os.Stat(p.Path); err != nil {
			t.Fatalf("%q does not exist", p.Path)
		}
	}
}
