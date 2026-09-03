package main

import (
	"errors"
	"path/filepath"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/inpututil"

	"github.com/FullFran/riffhero/internal/audio/codec"
	"github.com/FullFran/riffhero/internal/i18n"
	"github.com/FullFran/riffhero/internal/library"
	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/score"
)

// The file picker, for both a score and a backing track. One screen rather
// than two: they differ only in what they will let you open.

type browseState struct {
	kind    library.Kind
	dir     string
	listing library.Listing
	places  []library.Entry
	cursor  int
	scroll  int
	err     string
}

func (a *app) openBrowser(kind library.Kind) {
	a.browse.kind = kind
	a.browse.places = library.Places()

	dir := a.cfg.ScoreDir
	switch {
	case kind == library.Backing && a.backingPath != "":
		dir = filepath.Dir(a.backingPath)
	case kind == library.Score && a.scorePath != "":
		dir = filepath.Dir(a.scorePath)
	}
	if dir == "" && len(a.browse.places) > 0 {
		dir = a.browse.places[0].Path
	}

	a.browseTo(dir)
	a.mode = inSongs
}

func (a *app) browseTo(dir string) {
	listing, err := library.Scan(dir, a.browse.kind)
	if err != nil {
		// Our own wrapping is stripped so the reason can sit inside a
		// translated frame. What the operating system called it stays as it
		// came: "permission denied" is not ours to translate, and guessing at
		// it would be worse than leaving it in English.
		a.browse.err = i18n.Tf("could not read %s: %v", dir, reason(err))
		return
	}
	a.browse.err = ""
	a.browse.dir, a.browse.listing = listing.Dir, listing
	a.browse.cursor, a.browse.scroll = 0, 0
}

// browseRows is everything on the screen, in one list, so the mouse and the
// keyboard reach exactly the same things.
type browseRow struct {
	label string
	note  string
	dir   string // set for something to descend into
	file  string // set for something to open
	clear bool   // set for the row that removes the current backing track
}

// reason is the error underneath our own wrapping.
func reason(err error) error {
	if inner := errors.Unwrap(err); inner != nil {
		return inner
	}
	return err
}

func (a *app) browseRows() []browseRow {
	out := make([]browseRow, 0, len(a.browse.listing.Entries)+len(a.browse.places)+1)

	// Up first, then the shortcuts. It is the one most often wanted and the
	// one every other file browser puts at the top.
	if parent := a.browse.listing.Parent; parent != "" {
		out = append(out, browseRow{label: "..", note: i18n.T("up"), dir: parent})
	}
	// Choosing a backing track was a one-way door: nothing called clearBacking
	// and the choice persisted, so going back to practising unaccompanied meant
	// editing the config file by hand.
	if a.browse.kind == library.Backing && a.backingPath != "" {
		out = append(out, browseRow{label: i18n.T("NO BACKING TRACK"), note: i18n.T("practise unaccompanied"), clear: true})
	}
	for _, p := range a.browse.places {
		if p.Path == a.browse.dir {
			continue
		}
		out = append(out, browseRow{label: p.Name, note: i18n.T("place"), dir: p.Path})
	}
	for _, e := range a.browse.listing.Entries {
		if e.IsDir {
			out = append(out, browseRow{label: e.Name + "/", dir: e.Path})
			continue
		}
		out = append(out, browseRow{label: e.Name, note: e.Ext, file: e.Path})
	}
	return out
}

const browseRowHeight = 26

func (a *app) browseCapacity() int {
	n := (a.height - 180) / browseRowHeight
	if n < 3 {
		n = 3
	}
	return n
}

func (a *app) browseButtons(rows []browseRow) []row {
	x, w := 40.0, float64(a.width)-80
	top := 118.0

	out := make([]row, 0, a.browseCapacity())
	for i := a.browse.scroll; i < len(rows) && i < a.browse.scroll+a.browseCapacity(); i++ {
		out = append(out, row{
			x: x, y: top + float64(i-a.browse.scroll)*browseRowHeight,
			w: w, h: browseRowHeight - 3,
			label: rows[i].label, note: rows[i].note,
			chosen: i == a.browse.cursor,
		})
	}
	return out
}

func (a *app) updateBrowser() {
	rows := a.browseRows()

	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyEscape):
		a.openTitle()
		return
	case inpututil.IsKeyJustPressed(ebiten.KeyBackspace):
		if parent := a.browse.listing.Parent; parent != "" {
			a.browseTo(parent)
		}
		return
	}

	a.moveBrowseCursor(len(rows))

	chosen := -1
	if inpututil.IsKeyJustPressed(ebiten.KeyEnter) && a.browse.cursor < len(rows) {
		chosen = a.browse.cursor
	}
	for i, b := range a.browseButtons(rows) {
		if b.clicked() {
			chosen = a.browse.scroll + i
		}
	}
	if chosen < 0 || chosen >= len(rows) {
		return
	}

	switch picked := rows[chosen]; {
	case picked.dir != "":
		a.browseTo(picked.dir)
	case picked.clear:
		a.clearBacking()
		a.openTitle()
	case a.browse.kind == library.Score:
		a.chooseScore(picked.file)
	default:
		a.chooseBacking(picked.file)
	}
}

func (a *app) moveBrowseCursor(count int) {
	step := 0
	switch {
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowDown), inpututil.IsKeyJustPressed(ebiten.KeyJ):
		step = 1
	case inpututil.IsKeyJustPressed(ebiten.KeyArrowUp), inpututil.IsKeyJustPressed(ebiten.KeyK):
		step = -1
	case inpututil.IsKeyJustPressed(ebiten.KeyPageDown):
		step = a.browseCapacity()
	case inpututil.IsKeyJustPressed(ebiten.KeyPageUp):
		step = -a.browseCapacity()
	}
	if _, wheel := ebiten.Wheel(); wheel != 0 {
		step = -int(wheel) * 3
	}
	if step == 0 || count == 0 {
		return
	}

	a.browse.cursor += step
	switch {
	case a.browse.cursor < 0:
		a.browse.cursor = 0
	case a.browse.cursor >= count:
		a.browse.cursor = count - 1
	}
	// Keep the cursor on screen rather than scrolling it off the bottom, which
	// is what makes a long directory usable with the keyboard alone.
	if a.browse.cursor < a.browse.scroll {
		a.browse.scroll = a.browse.cursor
	}
	if last := a.browse.scroll + a.browseCapacity() - 1; a.browse.cursor > last {
		a.browse.scroll = a.browse.cursor - a.browseCapacity() + 1
	}
}

func (a *app) drawBrowser(screen *ebiten.Image) {
	screen.Fill(menuFace)

	heading := i18n.T("CHOOSE A SONG")
	if a.browse.kind == library.Backing {
		heading = i18n.T("CHOOSE A BACKING TRACK")
	}
	drawHeading(screen, heading, 40, 34)
	drawDim(screen, clip(a.browse.dir, (a.width-80)/glyphW), 40, 74)
	drawDim(screen, i18n.T("ENTER open    BACKSPACE up    ESC back"), 40, 92)

	rows := a.browseRows()
	buttons := a.browseButtons(rows)
	for _, b := range buttons {
		b.draw(screen)
	}

	switch {
	case a.browse.err != "":
		drawTinted(screen, "! "+a.browse.err, 40, a.height-40, menuBad)
	case len(rows) == 0:
		drawDim(screen, i18n.T("nothing here that can be opened"), 44, 124)
	case len(rows) > len(buttons):
		drawDim(screen, i18n.Tf("%d of %d", a.browse.scroll+len(buttons), len(rows)), 40, a.height-40)
	}
	if a.noticeTicks > 0 {
		drawTinted(screen, a.notice, 260, a.height-40, menuAccent)
	}
}

// chooseScore loads a score and rebuilds the run around it.
func (a *app) chooseScore(path string) {
	song, err := score.Load(path, a.clock)
	if err != nil {
		a.tell(i18n.Tf("could not open that score: %s", err), func() { a.mode = inSongs })
		return
	}
	track := song.GuitarTrack()
	if track < 0 {
		a.tell(i18n.Tf("%s has no playable notes", filepath.Base(path)), func() { a.mode = inSongs })
		return
	}

	a.song, a.track, a.scorePath = song, track, path
	a.cfg.Score, a.cfg.ScoreDir, a.cfg.Track = path, filepath.Dir(path), track
	a.loop = practice.Loop{}
	a.retimeSong()
	a.showNotice(i18n.Tf("loaded %s", song.Title))
	a.openTitle()
}

// chooseBacking decodes a track and hands it to whatever is playing.
func (a *app) chooseBacking(path string) {
	pcm, err := codec.DecodeFile(path)
	if err != nil {
		a.tell(i18n.Tf("could not open that backing track: %s", err), func() { a.mode = inSongs })
		return
	}

	a.backingPath = path
	a.cfg.Backing = path
	if err := a.setBacking(pcm.Conform(a.opts.sampleRate, 2).Data); err != nil {
		a.tell(i18n.Tf("the audio device would not restart: %s", err), a.openTitle)
		return
	}
	a.showNotice(i18n.Tf("backing: %s, %s", filepath.Base(path), formatSeconds(pcm.Duration())))
	a.openTitle()
}

// clearBacking goes back to practising unaccompanied.
func (a *app) clearBacking() {
	a.backingPath, a.cfg.Backing = "", ""
	if err := a.setBacking(nil); err != nil {
		a.tell(i18n.Tf("the audio device would not restart: %s", err), a.openTitle)
		return
	}
	a.showNotice(i18n.T("backing track removed"))
}

// retimeSong tells whatever is driving time how long the song now is, and
// rebuilds the scoreboard around the new notes.
func (a *app) retimeSong() {
	end := a.end(len(a.backing) / 2)
	if a.player != nil {
		a.player.SetEnd(end)
		a.player.SetLoop(practice.Loop{})
		a.player.Seek(0)
	} else {
		// A new score starts at the top with no region — chooseScore clears
		// a.loop to match — but the practice speed is the player's setting and
		// not the song's, so it comes across.
		speed := a.head.Speed()
		a.startScripted()
		a.head.SetSpeed(speed)
	}
	a.buildRunner()
}
