// Command riffhero renders the Phase 0 practice loop: a synthetic score, a
// frame-driven transport and a scripted detector standing in for real guitar
// input. No audio hardware is involved yet — see PLAN.md.
package main

import (
	"fmt"
	"image/color"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"

	"github.com/FullFran/riffhero/internal/practice"
	"github.com/FullFran/riffhero/internal/ui"
)

const (
	screenWidth  = 1100
	screenHeight = 650
	sampleRate   = 48000
	tailSeconds  = 1.0
)

var (
	colorBackground = color.RGBA{0x12, 0x14, 0x18, 0xff}
	colorString     = color.RGBA{0x3a, 0x40, 0x4a, 0xff}
	colorPlayhead   = color.RGBA{0xf2, 0xc0, 0x4c, 0xff}
	colorPending    = color.RGBA{0x8a, 0x93, 0xa4, 0xff}
	colorPerfect    = color.RGBA{0x4c, 0xd9, 0x7a, 0xff}
	colorGood       = color.RGBA{0x4c, 0x9a, 0xd9, 0xff}
	colorMiss       = color.RGBA{0xd9, 0x4c, 0x5a, 0xff}
)

type game struct {
	clock  practice.Clock
	layout ui.Layout
	song   []practice.Note

	transport *practice.Transport
	detector  *practice.ScriptedDetector
	session   *practice.Session
}

func newGame() *game {
	clock := practice.Clock{SampleRate: sampleRate}
	song := practice.SyntheticSong(clock)

	g := &game{
		clock:  clock,
		layout: ui.NewLayout(screenWidth, screenHeight, clock),
		song:   song,
	}
	g.reset()
	return g
}

// reset rebuilds the run so a restart scores from scratch.
func (g *game) reset() {
	g.transport = practice.NewTransport(g.clock, practice.SongEnd(g.song)+g.clock.Frames(tailSeconds))
	g.detector = practice.NewScriptedDetector(practice.Perform(g.song, g.performance()))
	g.session = practice.NewSession(g.song, practice.SessionConfig{
		Windows: practice.TimingWindows{
			Perfect: g.clock.Frames(0.050),
			Good:    g.clock.Frames(0.100),
		},
		MaxCents: 25,
	})
}

// performance is the fake player of Phase 0: mostly accurate, occasionally
// late, and it drops one note in four so misses are visible.
func (g *game) performance() []practice.Deviation {
	plan := make([]practice.Deviation, len(g.song))
	for i := range plan {
		switch i % 4 {
		case 0:
			plan[i] = practice.Deviation{Offset: g.clock.Frames(0.012)}
		case 1:
			plan[i] = practice.Deviation{Offset: g.clock.Frames(0.078), Cents: 9}
		case 2:
			plan[i] = practice.Deviation{Skip: true}
		case 3:
			plan[i] = practice.Deviation{Offset: -g.clock.Frames(0.030), Cents: -11}
		}
	}
	return plan
}

func (g *game) Update() error {
	if inpututil.IsKeyJustPressed(ebiten.KeyEscape) {
		return ebiten.Termination
	}
	if inpututil.IsKeyJustPressed(ebiten.KeyR) {
		g.reset()
		g.transport.Play()
	}
	if inpututil.IsKeyJustPressed(ebiten.KeySpace) {
		g.transport.TogglePlay()
	}

	tps := ebiten.TPS()
	if tps <= 0 {
		tps = 60
	}
	g.transport.AdvanceSeconds(1 / float64(tps))

	pos := g.transport.Position()
	for _, d := range g.detector.Poll(pos) {
		g.session.Feed(d)
	}
	g.session.Advance(pos)
	return nil
}

func (g *game) Draw(screen *ebiten.Image) {
	screen.Fill(colorBackground)

	now := g.transport.Position()
	g.drawStrings(screen)
	g.drawPlayhead(screen)
	g.drawNotes(screen, now)
	g.drawHUD(screen)
}

func (g *game) drawStrings(screen *ebiten.Image) {
	labels := [6]string{"e", "B", "G", "D", "A", "E"}
	for s := uint8(1); s <= 6; s++ {
		y := float32(g.layout.StringY(s))
		vector.StrokeLine(screen, 40, y, float32(g.layout.Width)-40, y, 1, colorString, false)
		ebitenutil.DebugPrintAt(screen, labels[s-1], 22, int(y)-7)
	}
}

func (g *game) drawPlayhead(screen *ebiten.Image) {
	x := float32(g.layout.PlayheadX)
	top := float32(g.layout.StringY(1)) - 24
	bottom := float32(g.layout.StringY(6)) + 24
	vector.StrokeLine(screen, x, top, x, bottom, 2, colorPlayhead, false)
	ebitenutil.DebugPrintAt(screen, "NOW", int(x)-10, int(bottom)+8)
}

func (g *game) drawNotes(screen *ebiten.Image, now practice.Frame) {
	from, to := g.layout.VisibleWindow(now)
	for _, v := range g.session.Upcoming(from, to) {
		x := float32(g.layout.NoteX(now, v.Note.Start))
		y := float32(g.layout.StringY(v.Note.String))

		c := colorPending
		if v.Resolved {
			c = ratingColor(v.Rating)
		}
		vector.DrawFilledCircle(screen, x, y, 11, c, true)
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%d", v.Note.Fret), int(x)-3, int(y)-7)
	}
}

func (g *game) drawHUD(screen *ebiten.Image) {
	st := g.session.Stats()

	ebitenutil.DebugPrintAt(screen, "RiffHero — Phase 0 (synthetic score, fake detector)", 24, 20)
	ebitenutil.DebugPrintAt(screen, "SPACE play/pause    R restart    ESC quit", 24, 40)

	rating := "—"
	if g.session.HasRating() {
		rating = ratingLabel(g.session.LastRating())
	}
	ebitenutil.DebugPrintAt(screen, rating, int(g.layout.PlayheadX)-20, int(g.layout.StringY(6))+40)

	hud := fmt.Sprintf(
		"accuracy %3.0f%%    combo x%-3d  best x%-3d    perfect %d  good %d  miss %d    %d/%d notes",
		st.Accuracy*100, st.Combo, st.MaxCombo, st.Perfect, st.Good, st.Miss, st.Resolved, st.Total,
	)
	ebitenutil.DebugPrintAt(screen, hud, 24, int(g.layout.Height)-36)

	state := "PAUSED"
	if g.transport.Playing() {
		state = "PLAYING"
	} else if g.transport.Finished() {
		state = "FINISHED — press R"
	}
	ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s  %6.2fs", state, g.clock.Seconds(g.transport.Position())), 24, int(g.layout.Height)-56)
}

func ratingColor(r practice.Rating) color.RGBA {
	switch r {
	case practice.Perfect:
		return colorPerfect
	case practice.Good:
		return colorGood
	default:
		return colorMiss
	}
}

func ratingLabel(r practice.Rating) string {
	switch r {
	case practice.Perfect:
		return "PERFECT"
	case practice.Good:
		return "GOOD"
	default:
		return "MISS"
	}
}

func (g *game) Layout(_, _ int) (int, int) { return screenWidth, screenHeight }

func main() {
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("RiffHero — Phase 0")
	if err := ebiten.RunGame(newGame()); err != nil {
		log.Fatal(err)
	}
}
