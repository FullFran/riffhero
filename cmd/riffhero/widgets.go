package main

import (
	"image/color"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
	"github.com/hajimehoshi/ebiten/v2/inpututil"
	"github.com/hajimehoshi/ebiten/v2/vector"
)

// The menu side of the app. Every screen is a stack of things that can be
// pressed, and every one of them can also be reached from the keyboard —
// RiffHero is played with a guitar in both hands, so a control that needs the
// mouse is a control that needs putting the guitar down.
//
// Widgets are values, not objects with state: where a button is and what it
// says are worked out afresh every frame from the app, so one can never show
// something the app no longer holds.

// The character cell of Ebiten's debug font, which everything here is measured
// in because it is the only font available without adding a dependency.
const (
	glyphW = 6
	glyphH = 13
	lineH  = 16
)

var (
	menuFace   = color.RGBA{0x12, 0x14, 0x18, 0xff}
	menuInk    = color.RGBA{0xe8, 0xe4, 0xd8, 0xff}
	menuDim    = color.RGBA{0x6a, 0x70, 0x82, 0xff}
	menuAccent = color.RGBA{0xf2, 0xc0, 0x4c, 0xff}
	menuGood   = color.RGBA{0x4c, 0xd9, 0x7a, 0xff}
	menuBad    = color.RGBA{0xd9, 0x4c, 0x5a, 0xff}

	buttonFace     = color.RGBA{0x23, 0x28, 0x30, 0xff}
	buttonEdge     = color.RGBA{0x3a, 0x40, 0x4a, 0xff}
	buttonHot      = color.RGBA{0x2e, 0x3a, 0x4a, 0xff}
	buttonPressed  = color.RGBA{0x3d, 0x55, 0x70, 0xff}
	buttonOffFace  = color.RGBA{0x19, 0x1c, 0x22, 0xff}
	buttonOffInk   = color.RGBA{0x4a, 0x4f, 0x5a, 0xff}
	buttonChosen   = color.RGBA{0x4c, 0xd9, 0x7a, 0xff}
	buttonBadgeInk = color.RGBA{0xf2, 0xc0, 0x4c, 0xff}
)

// button is one thing that can be pressed.
type button struct {
	x, y, w, h float64
	label      string

	// key is the keyboard shortcut that does the same thing, drawn in the
	// corner so it can be learned by using the mouse.
	key string
	// off greys it out and stops it answering.
	off bool
	// chosen marks the setting already in effect.
	chosen bool
	// badge is a short note in the corner: a value, a count, a warning.
	badge string
	// danger draws it in the colour of something that cannot be undone.
	danger bool
}

func (b button) contains(x, y int) bool {
	fx, fy := float64(x), float64(y)
	return fx >= b.x && fx <= b.x+b.w && fy >= b.y && fy <= b.y+b.h
}

func (b button) hovered() bool {
	return !b.off && b.contains(ebiten.CursorPosition())
}

func (b button) pressed() bool {
	return !b.off && ebiten.IsMouseButtonPressed(ebiten.MouseButtonLeft) &&
		b.contains(ebiten.CursorPosition())
}

// clicked reports whether the button has just been let go of.
//
// Released rather than pressed, so somebody can change their mind by sliding
// off it.
func (b button) clicked() bool {
	return !b.off && inpututil.IsMouseButtonJustReleased(ebiten.MouseButtonLeft) &&
		b.contains(ebiten.CursorPosition())
}

func (b button) draw(screen *ebiten.Image) {
	face, edge, ink := buttonFace, buttonEdge, menuInk
	switch {
	case b.off:
		face, edge, ink = buttonOffFace, buttonOffFace, buttonOffInk
	case b.pressed():
		face, edge = buttonPressed, menuInk
	case b.chosen:
		face, edge = buttonHot, buttonChosen
	case b.hovered():
		face = buttonHot
	}
	if b.danger && !b.off {
		edge = menuBad
	}

	vector.DrawFilledRect(screen, float32(b.x), float32(b.y), float32(b.w), float32(b.h), face, false)
	vector.StrokeRect(screen, float32(b.x), float32(b.y), float32(b.w), float32(b.h), 2, edge, false)

	drawTinted(screen, b.label, int(b.x+(b.w-textWidth(b.label))/2), int(b.y+(b.h-glyphH)/2), ink)

	if b.key != "" && !b.off {
		drawTinted(screen, b.key, int(b.x)+5, int(b.y)+4, menuDim)
	}
	if b.badge != "" {
		drawTinted(screen, b.badge, int(b.x+b.w-textWidth(b.badge))-6, int(b.y+b.h)-15, buttonBadgeInk)
	}
}

// row is a full-width line of a list: a device, a file, a track. It is a
// button with the label on the left and room for a note on the right, because
// a list of forty files centred on the screen is unreadable.
type row struct {
	x, y, w, h float64
	label      string
	note       string
	chosen     bool
	off        bool
}

func (r row) button() button {
	return button{x: r.x, y: r.y, w: r.w, h: r.h, off: r.off, chosen: r.chosen}
}

func (r row) clicked() bool { return r.button().clicked() }

func (r row) draw(screen *ebiten.Image) {
	b := r.button()
	face, edge, ink := buttonFace, buttonEdge, menuInk
	switch {
	case r.off:
		face, edge, ink = buttonOffFace, buttonOffFace, buttonOffInk
	case b.pressed():
		face, edge = buttonPressed, menuInk
	case r.chosen:
		face, edge = buttonHot, buttonChosen
	case b.hovered():
		face = buttonHot
	}

	vector.DrawFilledRect(screen, float32(r.x), float32(r.y), float32(r.w), float32(r.h), face, false)
	vector.StrokeRect(screen, float32(r.x), float32(r.y), float32(r.w), float32(r.h), 1, edge, false)

	textY := int(r.y + (r.h-glyphH)/2)
	drawTinted(screen, clip(r.label, int((r.w-16)/glyphW)), int(r.x)+10, textY, ink)
	if r.note != "" {
		drawTinted(screen, r.note, int(r.x+r.w-textWidth(r.note))-10, textY, menuDim)
	}
	if r.chosen {
		vector.DrawFilledRect(screen, float32(r.x), float32(r.y), 3, float32(r.h), buttonChosen, false)
	}
}

// stepper is a setting with a value between a minus and a plus. Sliders need a
// steady hand and a mouse; a stepper works from either, and the value is a
// number the player can read back to you.
type stepper struct {
	x, y, w, h float64
	label      string
	value      string
	atMin      bool
	atMax      bool
	keys       string // e.g. "[ ]", drawn under the label
}

const stepperButtonW = 40

func (s stepper) minus() button {
	return button{x: s.x + s.w - 2*stepperButtonW - 6, y: s.y, w: stepperButtonW, h: s.h, label: "-", off: s.atMin}
}

func (s stepper) plus() button {
	return button{x: s.x + s.w - stepperButtonW, y: s.y, w: stepperButtonW, h: s.h, label: "+", off: s.atMax}
}

func (s stepper) draw(screen *ebiten.Image) {
	textY := int(s.y + (s.h-glyphH)/2)
	drawTinted(screen, s.label, int(s.x), textY, menuInk)
	if s.keys != "" {
		drawTinted(screen, s.keys, int(s.x), textY+lineH, menuDim)
	}
	drawTinted(screen, s.value, int(s.x+s.w-2*stepperButtonW-16-textWidth(s.value)), textY, menuAccent)
	s.minus().draw(screen)
	s.plus().draw(screen)
}

// toggle is a setting that is either on or off.
type toggle struct {
	x, y, w, h float64
	label      string
	on         bool
	key        string
	note       string
}

func (t toggle) button() button {
	state := "OFF"
	if t.on {
		state = "ON"
	}
	return button{x: t.x, y: t.y, w: t.w, h: t.h, label: t.label + "  " + state, key: t.key, chosen: t.on}
}

func (t toggle) clicked() bool { return t.button().clicked() }

func (t toggle) draw(screen *ebiten.Image) {
	t.button().draw(screen)
	if t.note != "" {
		drawTinted(screen, t.note, int(t.x), int(t.y+t.h)+4, menuDim)
	}
}

// meter is a horizontal bar, for a level or a confidence.
func drawMeter(screen *ebiten.Image, x, y, w, h float64, fill float64, ink color.RGBA) {
	if fill < 0 {
		fill = 0
	}
	if fill > 1 {
		fill = 1
	}
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), colorMeterBed, false)
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w*fill), float32(h), ink, false)
}

// drawPanel is the plate a dialogue or a group of settings sits on.
func drawPanel(screen *ebiten.Image, x, y, w, h float64) {
	vector.DrawFilledRect(screen, float32(x), float32(y), float32(w), float32(h), colorPanel, false)
	vector.StrokeRect(screen, float32(x), float32(y), float32(w), float32(h), 1, buttonEdge, false)
}

// drawHeading is a screen's name, drawn at double size.
//
// Ebiten's debug font is six pixels wide, which is fine for a HUD read out of
// the corner of the eye and far too small for a menu somebody is looking
// straight at. Scaling the same glyphs is the only way to a larger one without
// carrying a font file.
func drawHeading(screen *ebiten.Image, text string, x, y int) {
	drawScaled(screen, text, x, y, 2, menuInk)
}

// tintedScratch is reused between calls: a menu writes dozens of lines a frame
// and a fresh texture for each of them would be waste.
var tintedScratch *ebiten.Image

// drawTinted writes one line of text in a colour, which the debug printer
// cannot do on its own.
func drawTinted(screen *ebiten.Image, text string, x, y int, ink color.RGBA) {
	if text == "" {
		return
	}
	scratch := tintedFor(text)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(ink)
	screen.DrawImage(scratch, op)
}

// drawScaled writes one line of text enlarged by a whole number.
func drawScaled(screen *ebiten.Image, text string, x, y, scale int, ink color.RGBA) {
	if text == "" || scale < 1 {
		return
	}
	scratch := tintedFor(text)
	op := &ebiten.DrawImageOptions{}
	op.GeoM.Scale(float64(scale), float64(scale))
	op.GeoM.Translate(float64(x), float64(y))
	op.ColorScale.ScaleWithColor(ink)
	screen.DrawImage(scratch, op)
}

func tintedFor(text string) *ebiten.Image {
	w, h := int(textWidth(text))+glyphW, lineH
	if tintedScratch == nil || tintedScratch.Bounds().Dx() < w {
		tintedScratch = ebiten.NewImage(w+256, h)
	}
	tintedScratch.Clear()
	ebitenutil.DebugPrintAt(tintedScratch, text, 0, 0)
	return tintedScratch
}

func drawDim(screen *ebiten.Image, text string, x, y int) {
	drawTinted(screen, text, x, y, menuDim)
}

func writeAt(screen *ebiten.Image, text string, x, y int) {
	drawTinted(screen, text, x, y, menuInk)
}

func textWidth(text string) float64 { return float64(len([]rune(text)) * glyphW) }

// clip shortens a label to fit, marking that it was cut. A file name that runs
// off the edge of its row is worse than one that admits it is too long.
func clip(text string, cells int) string {
	r := []rune(text)
	switch {
	case cells <= 0:
		// No room at all. Returning the whole string would draw it over
		// whatever is next to it, which is the one outcome worse than an
		// empty label.
		return ""
	case len(r) <= cells:
		return text
	case cells == 1:
		return string(r[:1])
	}
	return string(r[:cells-1]) + "…"
}
