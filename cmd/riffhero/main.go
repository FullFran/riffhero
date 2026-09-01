package main

import (
	"fmt"
	"log"

	"github.com/hajimehoshi/ebiten/v2"
	"github.com/hajimehoshi/ebiten/v2/ebitenutil"
)

const (
	screenWidth  = 1100
	screenHeight = 650
)

type game struct{}

func (g *game) Update() error { return nil }

func (g *game) Draw(screen *ebiten.Image) {
	ebitenutil.DebugPrintAt(screen, "RiffHero — Phase 0", 24, 24)
	ebitenutil.DebugPrintAt(screen, "score -> backing -> real guitar -> hit/miss", 24, 48)

	strings := []string{"E", "B", "G", "D", "A", "E"}
	for i, s := range strings {
		y := 150 + i*58
		ebitenutil.DebugPrintAt(screen, fmt.Sprintf("%s -----------------------------------------------------------", s), 80, y)
	}
	ebitenutil.DebugPrintAt(screen, "                         | NOW", 390, 510)
	ebitenutil.DebugPrintAt(screen, "Phase 0: synthetic timeline + matcher next", 300, 580)
}

func (g *game) Layout(_, _ int) (int, int) { return screenWidth, screenHeight }

func main() {
	ebiten.SetWindowSize(screenWidth, screenHeight)
	ebiten.SetWindowTitle("RiffHero")
	if err := ebiten.RunGame(&game{}); err != nil {
		log.Fatal(err)
	}
}
