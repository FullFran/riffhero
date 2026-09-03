package buildinfo

import (
	"os"
	"strings"
	"testing"
)

func TestVersionFallsBackToTheSourceConstant(t *testing.T) {
	// A `go test` binary has no injected version and a module version of
	// "(devel)", which is the same position a `go build` binary is in.
	if got := Version(); got != defaultVersion {
		t.Fatalf("got %q, want the source constant %q", got, defaultVersion)
	}
}

func TestVersionPrefersTheInjectedOneAndDropsTheV(t *testing.T) {
	// The tag is written v1.2.3 and the version reported is 1.2.3, so that
	// the two can be compared without either side having to remember which
	// convention the other uses.
	was := version
	t.Cleanup(func() { version = was })

	version = "v1.2.3"
	if got := Version(); got != "1.2.3" {
		t.Fatalf("got %q", got)
	}
	version = "1.2.3"
	if got := Version(); got != "1.2.3" {
		t.Fatalf("got %q", got)
	}
}

func TestStringNamesTheProgramAndTheVersion(t *testing.T) {
	got := String()
	if !strings.HasPrefix(got, "riffhero "+Version()) {
		t.Fatalf("got %q", got)
	}
	// Always says which Go and which platform, whatever the VCS stamping did.
	if !strings.Contains(got, "go1.") {
		t.Fatalf("no Go version in %q", got)
	}
	if !strings.Contains(got, "/") {
		t.Fatalf("no platform in %q", got)
	}
}

func TestAPseudoVersionIsNotAVersion(t *testing.T) {
	// A plain `go build` in a working tree makes Go invent
	// "0.0.0-20260903095954-2e08fe249586+dirty" out of the commit. It is not a
	// release, nobody would call the build that, and printing it hides the
	// only honest answer, which is the version in the source.
	for _, v := range []string{
		"0.0.0-20260903095954-2e08fe249586+dirty",
		"v0.0.0-20260903095954-2e08fe249586",
		"v1.2.4-0.20260903095954-2e08fe249586",
	} {
		if !isPseudo(v) {
			t.Errorf("%q should be recognised as a pseudo-version", v)
		}
	}
	for _, v := range []string{"v1.2.3", "1.2.3", "v1.2.3-rc.1", "(devel)"} {
		if isPseudo(v) {
			t.Errorf("%q is a real version", v)
		}
	}
}

func TestVersionMatchesTheReleaseTag(t *testing.T) {
	// The release workflow sets this to the tag it is about to publish.
	//
	// A tag that disagrees with the version in the source ships a binary that
	// lies about which release it is, and the only fix once it is published is
	// to yank it. This is the cheapest possible place to catch that: no
	// display, no C toolchain, no build.
	tag := os.Getenv("RIFFHERO_RELEASE_TAG")
	if tag == "" {
		t.Skip("not a release build")
	}
	// A prerelease tag (v1.0.0-rc.1) is cut from the same tree as the release
	// it rehearses, so only the base versions have to agree.
	want := strings.TrimPrefix(tag, "v")
	if base, _, found := strings.Cut(want, "-"); found {
		want = base
	}
	if want != defaultVersion {
		t.Fatalf("tag %s does not match defaultVersion %q in internal/buildinfo/buildinfo.go.\n"+
			"Bump defaultVersion, commit, then retag.", tag, defaultVersion)
	}
}
