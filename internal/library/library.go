// Package library lists what is on disk that the app could open.
//
// It exists so the song picker does not have to know what a score file looks
// like, and so that knowledge stays in the one package that already has it.
package library

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/FullFran/riffhero/internal/score"
)

// Kind says what a file is good for.
type Kind uint8

const (
	Score Kind = iota
	Backing
)

// backingExtensions are what internal/audio/codec can decode.
var backingExtensions = []string{".wav", ".mp3", ".flac"}

// Entry is one thing the browser can show.
type Entry struct {
	Path  string // absolute where it can be made so
	Name  string // base name without its extension, for display
	Ext   string // lower-case, with the dot
	Kind  Kind
	Size  int64
	IsDir bool
}

// Listing is one directory's worth, already ordered: directories first, then
// files, each alphabetically and ignoring case. A browser that lists in
// whatever order the filesystem hands back is one nobody can find anything in.
type Listing struct {
	Dir     string
	Parent  string // the directory above, empty at the root
	Entries []Entry
}

// Scan lists the directories and the openable files in dir.
//
// A path that is a file rather than a directory scans its parent, because a
// path pasted from somewhere else is as likely to be one as the other.
func Scan(dir string, kinds ...Kind) (Listing, error) {
	if dir == "" {
		dir = "."
	}
	if abs, err := filepath.Abs(dir); err == nil {
		dir = abs
	}
	if info, err := os.Stat(dir); err == nil && !info.IsDir() {
		dir = filepath.Dir(dir)
	}

	items, err := os.ReadDir(dir)
	if err != nil {
		return Listing{}, fmt.Errorf("reading %s: %w", dir, err)
	}

	listing := Listing{Dir: dir}
	if parent := filepath.Dir(dir); parent != dir {
		listing.Parent = parent
	}

	for _, item := range items {
		name := item.Name()
		// A browser that opens on somebody's home directory and lists forty
		// dotfiles is a browser they close again.
		if strings.HasPrefix(name, ".") {
			continue
		}
		path := filepath.Join(dir, name)

		// Stat rather than trusting the dir entry, so a symlink to a directory
		// reads as one. A broken link stats with an error and is simply left
		// out rather than stopping the scan.
		info, err := os.Stat(path)
		if err != nil {
			continue
		}
		if info.IsDir() {
			listing.Entries = append(listing.Entries, Entry{
				Path: path, Name: name, IsDir: true,
			})
			continue
		}

		kind, ok := KindOf(path)
		if !ok || !wanted(kind, kinds) {
			continue
		}
		ext := strings.ToLower(filepath.Ext(name))
		listing.Entries = append(listing.Entries, Entry{
			Path: path,
			Name: strings.TrimSuffix(name, filepath.Ext(name)),
			Ext:  ext,
			Kind: kind,
			Size: info.Size(),
		})
	}

	sort.SliceStable(listing.Entries, func(i, j int) bool {
		a, b := listing.Entries[i], listing.Entries[j]
		if a.IsDir != b.IsDir {
			return a.IsDir
		}
		ai, bi := strings.ToLower(a.Name), strings.ToLower(b.Name)
		if ai != bi {
			return ai < bi
		}
		return a.Name < b.Name
	})
	return listing, nil
}

// KindOf reports what a path's extension makes it, and whether it is anything
// the app can open at all.
func KindOf(path string) (Kind, bool) {
	ext := strings.ToLower(filepath.Ext(path))
	for _, e := range score.Extensions {
		if ext == e {
			return Score, true
		}
	}
	for _, e := range backingExtensions {
		if ext == e {
			return Backing, true
		}
	}
	return Score, false
}

// Places are the directories worth offering first. Only ones that are really
// there are returned, in the order somebody would look in them, without
// repeats — a home directory that is also the working directory should appear
// once.
func Places() []Entry {
	var out []Entry
	seen := map[string]bool{}

	add := func(name, path string) {
		if path == "" {
			return
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return
		}
		if seen[abs] {
			return
		}
		if info, err := os.Stat(abs); err != nil || !info.IsDir() {
			return
		}
		seen[abs] = true
		out = append(out, Entry{Path: abs, Name: name, IsDir: true})
	}

	if wd, err := os.Getwd(); err == nil {
		add("here", wd)
	}
	home, _ := os.UserHomeDir()
	add("music", xdgDir("XDG_MUSIC_DIR", home, "Music"))
	add("documents", xdgDir("XDG_DOCUMENTS_DIR", home, "Documents"))
	add("downloads", xdgDir("XDG_DOWNLOAD_DIR", home, "Downloads"))
	add("home", home)
	return out
}

// xdgDir resolves one of the user's well-known directories.
//
// The English fallback is the last resort and not the usual answer. On a
// desktop in any other language the music folder is called Música or Musik or
// 音楽, and guessing "Music" finds nothing — which is how a song picker ends
// up offering only the home directory. The name lives in user-dirs.dirs, so
// that is where it is read from.
func xdgDir(env, home, fallback string) string {
	if dir := os.Getenv(env); dir != "" {
		return dir
	}
	if dir := userDirs(home)[env]; dir != "" {
		return dir
	}
	if home == "" {
		return ""
	}
	return filepath.Join(home, fallback)
}

// userDirsCache is read once: the file does not change while the app is up,
// and Places is called every time the browser opens.
var (
	userDirsCache map[string]string
	userDirsRead  bool
)

// userDirs parses ~/.config/user-dirs.dirs, whose format is documented in its
// own header as KEY="$HOME/name" or KEY="/absolute/name" and nothing else.
func userDirs(home string) map[string]string {
	if userDirsRead {
		return userDirsCache
	}
	userDirsRead = true
	userDirsCache = map[string]string{}

	base := os.Getenv("XDG_CONFIG_HOME")
	if base == "" {
		if home == "" {
			return userDirsCache
		}
		base = filepath.Join(home, ".config")
	}
	data, err := os.ReadFile(filepath.Join(base, "user-dirs.dirs"))
	if err != nil {
		return userDirsCache
	}

	for _, line := range strings.Split(string(data), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		key, value, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		value = strings.Trim(strings.TrimSpace(value), `"`)
		if value == "" {
			continue
		}
		if rest, cut := strings.CutPrefix(value, "$HOME/"); cut {
			if home == "" {
				continue
			}
			value = filepath.Join(home, rest)
		}
		userDirsCache[strings.TrimSpace(key)] = value
	}
	return userDirsCache
}

// wanted reports whether a kind passes the filter. No filter means everything.
func wanted(kind Kind, kinds []Kind) bool {
	if len(kinds) == 0 {
		return true
	}
	for _, k := range kinds {
		if k == kind {
			return true
		}
	}
	return false
}
