package i18n

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// restore puts the language back, so one test cannot decide what another one
// reads. The package keeps its language in a global on purpose — every screen
// asks for it hundreds of times a frame — and a global is exactly what needs
// this.
func restore(t *testing.T) {
	t.Helper()
	was := Current()
	t.Cleanup(func() { Use(was) })
}

func TestEnglishIsTheIdentity(t *testing.T) {
	restore(t)
	Use(English)
	if got := T("CHOOSE A SONG"); got != "CHOOSE A SONG" {
		t.Fatalf("got %q", got)
	}
}

func TestAnUntranslatedLineComesBackInEnglish(t *testing.T) {
	// The whole reason the English is the key: a missing entry is a line
	// somebody can still read, not "menu.title.play" where the words go.
	restore(t)
	Use(Spanish)
	if got := T("a line nobody has translated"); got != "a line nobody has translated" {
		t.Fatalf("got %q", got)
	}
}

func TestTfKeepsTheValues(t *testing.T) {
	restore(t)
	Use(English)
	if got := Tf("%d of %d", 3, 7); got != "3 of 7" {
		t.Fatalf("got %q", got)
	}
}

func TestPlural(t *testing.T) {
	cases := map[int]string{0: "many", 1: "one", 2: "many", -1: "one", 12: "many"}
	for n, want := range cases {
		if got := Plural(n, "one", "many"); got != want {
			t.Fatalf("%d = %q, want %q", n, got, want)
		}
	}
}

func TestUseRepairsALanguageWeDoNotSpeak(t *testing.T) {
	// A hand-edited config saying "language": "kl" must not leave the app
	// drawing nothing.
	restore(t)
	Use(Lang("kl"))
	if Current() != Default {
		t.Fatalf("got %q", Current())
	}
}

func TestNextCycles(t *testing.T) {
	seen := map[Lang]bool{}
	l := English
	for range Languages() {
		if seen[l] {
			t.Fatalf("%q came round twice", l)
		}
		seen[l] = true
		l = Next(l)
	}
	if l != English {
		t.Fatalf("the cycle ended on %q", l)
	}
	if got := Next(Lang("nonsense")); !got.Valid() {
		t.Fatalf("got %q", got)
	}
}

func TestEndonymIsInItsOwnLanguage(t *testing.T) {
	// "Spanish" means nothing to somebody who reads no English, which is the
	// only person the row is there for.
	if got := Spanish.Endonym(); got != "Español" {
		t.Fatalf("got %q", got)
	}
	if got := English.Endonym(); got != "English" {
		t.Fatalf("got %q", got)
	}
}

func TestDetect(t *testing.T) {
	cases := []struct {
		env  map[string]string
		want Lang
	}{
		{map[string]string{"LANG": "es_ES.UTF-8"}, Spanish},
		{map[string]string{"LANG": "es"}, Spanish},
		{map[string]string{"LANG": "es_AR.UTF-8@euro"}, Spanish},
		{map[string]string{"LANG": "en_GB.UTF-8"}, English},
		{map[string]string{"LANG": "de_DE.UTF-8"}, English}, // nothing better to offer
		{map[string]string{}, English},
		{map[string]string{"LANG": "C"}, English},
		{map[string]string{"LANG": "POSIX"}, English},
		// POSIX order: LC_ALL beats LC_MESSAGES beats LANG.
		{map[string]string{"LC_ALL": "es_ES.UTF-8", "LANG": "en_GB.UTF-8"}, Spanish},
		{map[string]string{"LC_MESSAGES": "es_ES.UTF-8", "LANG": "en_GB.UTF-8"}, Spanish},
		{map[string]string{"LC_ALL": "en_GB.UTF-8", "LC_MESSAGES": "es_ES.UTF-8"}, English},
		// An empty variable is not an answer; the next one is asked.
		{map[string]string{"LC_ALL": "", "LANG": "es_ES.UTF-8"}, Spanish},
		// LANGUAGE is what somebody sets to run a Spanish desktop in English,
		// so it has to win, and it may hold a whole list of preferences.
		{map[string]string{"LANGUAGE": "en", "LANG": "es_ES.UTF-8"}, English},
		{map[string]string{"LANGUAGE": "es", "LANG": "en_GB.UTF-8"}, Spanish},
		{map[string]string{"LANGUAGE": "pt:es:en", "LANG": "en_GB.UTF-8"}, Spanish},
		{map[string]string{"LANGUAGE": "C", "LANG": "es_ES.UTF-8"}, Spanish},
		{map[string]string{"LANGUAGE": "es-ES", "LANG": "en_GB.UTF-8"}, Spanish},
		// A tag we do not speak stops the search: it is still an answer.
		{map[string]string{"LANGUAGE": "de", "LANG": "es_ES.UTF-8"}, English},
	}
	for _, c := range cases {
		got := Detect(func(k string) string { return c.env[k] })
		if got != c.want {
			t.Fatalf("%v = %q, want %q", c.env, got, c.want)
		}
	}
}

// ---------------------------------------------------------------- the catalogue

func TestEveryTranslationFitsTheFont(t *testing.T) {
	// Ebiten's debug font is a 32-by-8 atlas of the first 256 code points.
	// Anything past U+00FF draws nothing at all and still advances the cursor,
	// so a smart quote is a hole in the line. Every accent Spanish needs is
	// inside Latin-1 — the whole reason the app can be translated without
	// carrying a font file — but an em dash or a curly apostrophe is not, and
	// they are exactly what a translation picks up.
	for _, l := range Languages() {
		for key, value := range catalogue(l) {
			for _, r := range value {
				if r > 0xff {
					t.Errorf("%s: %q -> %q contains %q, past U+00FF, which draws as a gap", l, key, value, r)
					break
				}
			}
		}
	}
}

func TestNoTranslationIsEmpty(t *testing.T) {
	// An empty value is worse than a missing one: a missing entry falls back
	// to English, an empty one blanks the line.
	for _, l := range Languages() {
		for key, value := range catalogue(l) {
			if strings.TrimSpace(value) == "" {
				t.Errorf("%s: %q translates to nothing", l, key)
			}
		}
	}
}

func TestTranslationsKeepTheirFormatVerbs(t *testing.T) {
	// A translation may move the values around — Spanish does, constantly —
	// but dropping one leaves "%!d(MISSING)" on the screen and inventing one
	// leaves "%!s(EXTRA)".
	for _, l := range Languages() {
		for key, value := range catalogue(l) {
			if got, want := verbs(value), verbs(key); !sameVerbs(got, want) {
				t.Errorf("%s: %q has %v, but %q has %v", l, key, want, value, got)
			}
		}
	}
}

// verbs pulls the printf verbs out of a format string, in order.
func verbs(s string) []string {
	var out []string
	for i := 0; i < len(s); i++ {
		if s[i] != '%' {
			continue
		}
		j := i + 1
		for j < len(s) && strings.ContainsRune("+-# 0123456789.", rune(s[j])) {
			j++
		}
		// An explicit argument index, "%-3[4]d", sits between the width and
		// the verb. It is the whole reason a translation may reorder the
		// values, so the scanner has to step over it rather than call "[" the
		// verb and stop looking.
		if j < len(s) && s[j] == '[' {
			if k := strings.IndexByte(s[j:], ']'); k > 0 {
				j += k + 1
			}
		}
		if j >= len(s) {
			break
		}
		if s[j] == '%' { // an escaped per cent is not a value
			i = j
			continue
		}
		out = append(out, string(s[j]))
		i = j
	}
	return out
}

func sameVerbs(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	// Order may differ — that is the point of translating a format string —
	// but the multiset must match.
	count := map[string]int{}
	for _, v := range a {
		count[v]++
	}
	for _, v := range b {
		count[v]--
	}
	for _, n := range count {
		if n != 0 {
			return false
		}
	}
	return true
}

func TestEveryTranslationStillMatchesSomethingInTheSource(t *testing.T) {
	// The price of keying on the English: editing a line in the drawing code
	// orphans its translation, and nothing at run time would ever say so. The
	// entry just stops being found and the screen quietly goes back to
	// English. This is the test that notices.
	src := sourceText(t)
	for _, l := range Languages() {
		for key := range catalogue(l) {
			if !strings.Contains(src, quoteFor(key)) {
				t.Errorf("%s: %q is not asked for anywhere any more; the English was probably edited", l, key)
			}
		}
	}
}

func quoteFor(s string) string {
	// The source writes these as ordinary interpreted string literals, so the
	// only escaping in play is the quote and the backslash.
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\t", `\t`)
	return `"` + r.Replace(s) + `"`
}

// sourceText is every non-test Go file in the app, concatenated.
func sourceText(t *testing.T) string {
	t.Helper()
	root := filepath.Join("..", "..")
	var b strings.Builder
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		switch {
		case err != nil:
			return err
		case info.IsDir() && (info.Name() == ".git" || info.Name() == "bin"):
			return filepath.SkipDir
		case info.IsDir(), !strings.HasSuffix(path, ".go"), strings.HasSuffix(path, "_test.go"):
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		b.Write(data)
		b.WriteByte('\n')
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if b.Len() == 0 {
		t.Fatal("read no source at all")
	}
	return b.String()
}

func TestVerbsReadsAnExplicitArgumentIndex(t *testing.T) {
	// "%-3[4]d" is how a translation is allowed to move the values around, and
	// if the scanner cannot read it the format check silently passes anything.
	got := verbs("accuracy %3.0[1]f%%   perfect %-3[4]d   %[7]d/%[8]d")
	want := []string{"f", "d", "d", "d"}
	if !sameVerbs(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	if v := verbs("%% is not a value"); len(v) != 0 {
		t.Fatalf("an escaped per cent is not a value: %v", v)
	}
}
