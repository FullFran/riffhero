// Package buildinfo answers one question: which RiffHero is this.
//
// It has to be answerable from a bug report, which means it has to be right
// for all three ways a binary gets made, and they do not agree:
//
//	a release      built by CI from a tag, version injected at link time
//	go install     version comes from the module proxy, nothing is injected
//	go build       neither, and the tree may have uncommitted changes
//
// Go stamps the commit, the commit time and whether the tree was dirty into
// every binary by itself, and has since 1.18 - that part needs no help and
// cannot drift, because it is not a number anybody types. The semantic
// version is the part Go knows nothing about, because Go has no concept of a
// tag, and so it is the only part injected by hand.
package buildinfo

import (
	"fmt"
	"regexp"
	"runtime"
	"runtime/debug"
	"strings"
)

// version is set at link time by the release workflow:
//
//	-ldflags "-X github.com/FullFran/riffhero/internal/buildinfo.version=v1.2.3"
var version string

// defaultVersion is what a build from a working tree reports. The release
// workflow refuses to publish a tag that disagrees with it, so the two cannot
// drift apart without somebody being told.
const defaultVersion = "0.1.0"

// Version is the semantic version, without a leading v.
func Version() string {
	if version != "" {
		return strings.TrimPrefix(version, "v")
	}
	// A `go install ...@v1.2.3` build has no injected version but the module
	// system knows which one it fetched.
	//
	// It also invents one when nobody asked. A plain `go build` in a working
	// tree reports "0.0.0-20260903095954-2e08fe249586+dirty" - a pseudo-version
	// synthesised from the commit, which is not a release, is not what anybody
	// would call this build, and is a worse answer than saying so.
	if bi, ok := debug.ReadBuildInfo(); ok {
		if v := bi.Main.Version; v != "" && v != "(devel)" && !isPseudo(v) {
			return strings.TrimPrefix(v, "v")
		}
	}
	return defaultVersion
}

// pseudoVersion is Go's stand-in for a tag that does not exist: a base
// version, a commit timestamp and a short revision, optionally marked dirty.
// The separator before the timestamp is a dash when there is no base tag and
// a dot when the base is a real one, which is why it matches both.
var pseudoVersion = regexp.MustCompile(`[-.][0-9]{14}-[0-9a-f]{12}(\+dirty)?$`)

func isPseudo(v string) bool { return pseudoVersion.MatchString(v) }

// Commit is the revision the binary was built from, shortened, and whether
// the tree had uncommitted changes at the time.
func Commit() (sha string, dirty bool) {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return "", false
	}
	for _, s := range bi.Settings {
		switch s.Key {
		case "vcs.revision":
			sha = s.Value
			if len(sha) > 12 {
				sha = sha[:12]
			}
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	return sha, dirty
}

// BuiltAt is when the revision was committed, which for a release built by CI
// from a clean checkout is as good as when it was built.
func BuiltAt() string {
	bi, ok := debug.ReadBuildInfo()
	if !ok {
		return ""
	}
	for _, s := range bi.Settings {
		if s.Key == "vcs.time" {
			return s.Value
		}
	}
	return ""
}

// String is the one line --version prints.
func String() string {
	var b strings.Builder
	fmt.Fprintf(&b, "riffhero %s", Version())

	var parts []string
	if sha, dirty := Commit(); sha != "" {
		if dirty {
			sha += "-dirty"
		}
		parts = append(parts, "commit "+sha)
	}
	if at := BuiltAt(); at != "" {
		parts = append(parts, "built "+at)
	}
	parts = append(parts, runtime.Version(), runtime.GOOS+"/"+runtime.GOARCH)

	fmt.Fprintf(&b, " (%s)", strings.Join(parts, ", "))
	return b.String()
}
