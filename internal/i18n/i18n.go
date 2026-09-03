// Package i18n is the app's own words, in the player's language.
//
// The English text is the key. That is unusual — most catalogues key on a
// short symbol — and it is deliberate: a line with no translation comes back
// in English, which is a bad day for a Spanish speaker and a far better one
// than "menu.title.play" where the words should be. It also keeps the drawing
// code reading like the screen it draws.
//
// The cost is that editing the English orphans its translation silently, so a
// test walks the source and says which entries no longer match anything.
package i18n

import (
	"fmt"
	"strings"
	"sync/atomic"
)

// Lang is a language the app speaks.
type Lang string

const (
	English Lang = "en"
	Spanish Lang = "es"
)

// Default is what an unrecognised or missing choice reads as.
const Default = English

// current is read on the drawing path several hundred times a frame and
// written once, by somebody pressing a key. An atomic pointer to a whole table
// costs nothing to read and means the write cannot be seen half-done — which
// matters because the language can also be switched while the audio callback
// is running.
var current atomic.Pointer[table]

type table struct {
	lang  Lang
	words map[string]string
}

func init() { Use(Default) }

// Use switches language. Anything drawn after it comes back in the new one;
// nothing needs rebuilding, because every screen works its text out afresh
// every frame.
func Use(l Lang) {
	current.Store(&table{lang: l.orDefault(), words: catalogue(l.orDefault())})
}

// Current is the language in use.
func Current() Lang { return current.Load().lang }

// T is one line of text.
func T(en string) string {
	t := current.Load()
	if s, ok := t.words[en]; ok {
		return s
	}
	return en
}

// Tf is T for a line with values in it.
//
// The English format string is the key, so the translation may move the values
// around — Spanish does, constantly — as long as it keeps the same verbs.
func Tf(format string, args ...any) string {
	return fmt.Sprintf(T(format), args...)
}

// Plural picks the singular or the plural form, which is then translated by T
// or Tf like any other line. English and Spanish agree on where the boundary
// falls: one is singular and everything else, nought included, is not.
func Plural(n int, one, many string) string {
	if n == 1 || n == -1 {
		return one
	}
	return many
}

// Valid reports whether l is a language the app has words for.
func (l Lang) Valid() bool {
	switch l {
	case English, Spanish:
		return true
	}
	return false
}

func (l Lang) orDefault() Lang {
	if l.Valid() {
		return l
	}
	return Default
}

// Endonym is the language's name in itself, never translated. A menu that
// offers "Spanish" to somebody who reads no English is offering nothing.
func (l Lang) Endonym() string {
	switch l.orDefault() {
	case Spanish:
		return "Español"
	default:
		return "English"
	}
}

// Languages are the ones on offer, in a stable order.
func Languages() []Lang { return []Lang{English, Spanish} }

// Next cycles, so one key can switch between them.
func Next(l Lang) Lang {
	all := Languages()
	for i, c := range all {
		if c == l.orDefault() {
			return all[(i+1)%len(all)]
		}
	}
	return Default
}

// Detect reads the language out of the environment, the way every other
// program on the machine does.
//
// POSIX order: LC_ALL wins over LC_MESSAGES, which wins over LANG. A value
// looks like "es_ES.UTF-8" or "es", and "C" or "POSIX" mean no preference
// rather than a language called C.
func Detect(lookup func(string) string) Lang {
	for _, name := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		v := strings.TrimSpace(lookup(name))
		if v == "" || v == "C" || v == "POSIX" {
			continue
		}
		// es_ES.UTF-8@euro -> es
		v, _, _ = strings.Cut(v, "@")
		v, _, _ = strings.Cut(v, ".")
		v, _, _ = strings.Cut(v, "_")
		if l := Lang(strings.ToLower(v)); l.Valid() {
			return l
		}
		// A language we do not speak is still an answer: the player has said
		// what they want and it is not English by default. There is nothing
		// better to give them than English, but there is no point reading the
		// next variable either.
		return Default
	}
	return Default
}

func catalogue(l Lang) map[string]string {
	if l == Spanish {
		return spanish
	}
	return nil
}
