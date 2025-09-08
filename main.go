package main

import (
	"context"
	"flag"
	"fmt"
	"io"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"
)

// Color represents RGB color values.
type Color struct{ R, G, B uint8 }

// Matrix character sets
var matrixCharSets = map[string][]rune{
	"matrix":   []rune("λｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ"),
	"binary":   []rune("01"),
	"symbols":  []rune("!@#$%^&*()_+-=[]{}|;':\",./<>?"),
	"emojis":   []rune("😂😅😊😂🔥💯✨🤷‍♂️🚀🎉🌟🌈🍕🍔🍟🍦📚💡⚽️🏀🎾🏐🏈🏉🏸🏓🏒🏑🏏🏹🎣🥊🥋🎽🏅🎖🏆🎫🎨🎬🎧🎤"),
	"kanji":    []rune("書道日本漢字文化侍"),
	"greek":    []rune("αβγδεζηθικλμνξοπρστυφχψω"),
	"cyrillic": []rune("абвгдежзийклмнопрстуфхцчшщъыьэюя"),
}

var matrixRunes = matrixCharSets["matrix"] // Default character set

// Drop represents a single falling character stream.
type Drop struct {
	pos    int
	length int
	char   rune
	active bool
}

// DropFactory defines the interface for creating Drop objects.
type DropFactory interface {
	CreateDrop(screenHeight int) Drop
}

// RandomDropFactory is a concrete implementation of DropFactory.
type RandomDropFactory struct {
	randGen      *rand.Rand
	screenHeight int
}

// NewRandomDropFactory creates a new RandomDropFactory.
func NewRandomDropFactory(randGen *rand.Rand, screenHeight int) *RandomDropFactory {
	return &RandomDropFactory{
		randGen:      randGen,
		screenHeight: screenHeight,
	}
}

// CreateDrop initializes a new drop with random properties.
func (f *RandomDropFactory) CreateDrop(screenHeight int) Drop {
	return Drop{
		pos:    f.randGen.Intn(screenHeight) - f.randGen.Intn(screenHeight/2),
		length: f.randGen.Intn(12) + 8,
		char:   getRandomMatrixChar(f.randGen),
		active: true,
	}
}

// MatrixEngine manages the entire animation logic.
type MatrixEngine struct {
	screen       *Screen
	drops        []Drop
	baseColor    Color
	trailColors  []Color
	density      float64
	animationCtx context.Context
	randGen      *rand.Rand
	dropFactory  DropFactory
}

// NewMatrixEngine creates a new instance of the MatrixEngine.
func NewMatrixEngine(ctx context.Context, height, width int, baseColor Color, density float64, randGen *rand.Rand, dropFactory DropFactory) *MatrixEngine {
	engine := &MatrixEngine{
		screen:       NewScreen(height, width),
		baseColor:    baseColor,
		density:      density,
		animationCtx: ctx,
		randGen:      randGen,
		dropFactory:  dropFactory,
	}
	engine.trailColors = engine.calculateTrailColors(6)
	engine.drops = engine.createDrops(width)
	return engine
}

// createDrops initializes the drop streams.
func (me *MatrixEngine) createDrops(count int) []Drop {
	totalDrops := int(float64(count) * me.density)
	if totalDrops < count {
		totalDrops = count
	}

	drops := make([]Drop, totalDrops)
	for i := range drops {
		drops[i] = me.dropFactory.CreateDrop(me.screen.height)
	}
	return drops
}

// updateAndRender updates the drops and renders the screen.
func (me *MatrixEngine) updateAndRender(previousScreen *Screen) {
	// Handle terminal resize.
	if me.screen.checkAndResize() {
		me.drops = me.resizeDrops(me.screen.width, me.screen.height)
		me.screen.renderFull() // Force full redraw on resize
		me.screen.deepCopy(previousScreen)
		return
	}

	me.screen.clear()

	// Update and draw all drops.
	dropIndex := 0
	for col := 0; col < me.screen.width; col++ {
		// Determine how many drops should be drawn in this specific column.
		dropsPerColumn := int(me.density)
		if me.randGen.Float64() < (me.density - float64(dropsPerColumn)) {
			dropsPerColumn++
		}
		if dropsPerColumn < 1 {
			dropsPerColumn = 1
		}

		// Update and draw each drop.
		for i := 0; i < dropsPerColumn; i++ {
			if dropIndex < len(me.drops) {
				me.drops[dropIndex].update(me.screen.height, me.density, me.randGen)
				me.drops[dropIndex].draw(me.screen, col, me.trailColors)
				dropIndex++
			}
		}
	}

	// Render only the changes to the screen for performance.
	me.screen.renderChanges(previousScreen)

	// Update the previous screen buffer for the next frame.
	me.screen.deepCopy(previousScreen)
}

// resizeDrops adjusts the number of drops on a terminal resize.
func (me *MatrixEngine) resizeDrops(newWidth, screenHeight int) []Drop {
	newTotalDrops := int(float64(newWidth) * me.density)
	if newTotalDrops < newWidth {
		newTotalDrops = newWidth
	}
	if newTotalDrops > len(me.drops) {
		for i := len(me.drops); i < newTotalDrops; i++ {
			me.drops = append(me.drops, me.dropFactory.CreateDrop(screenHeight))
		}
	} else if newTotalDrops < len(me.drops) {
		me.drops = me.drops[:newTotalDrops]
	}
	return me.drops
}

// calculateTrailColors generates the color gradient.
func (me *MatrixEngine) calculateTrailColors(steps int) []Color {
	colors := make([]Color, steps)
	colors[0] = brightenColor(me.baseColor, 1.2)

	for i := 1; i < steps; i++ {
		fade := 1.0 - (float64(i)/float64(steps-1))*0.7
		colors[i] = dimColor(me.baseColor, fade)
	}
	return colors
}

// Drop represents a single falling character stream.
func (d *Drop) update(screenHeight int, density float64, randGen *rand.Rand) {
	if !d.active {
		activationChance := 0.005 * density
		if density > 1.0 {
			activationChance = 0.005 + (density-1.0)*0.02
		}
		if randGen.Float64() < activationChance {
			d.active = true
			d.pos = 0
			d.length = randGen.Intn(12) + 8
			d.char = getRandomMatrixChar(randGen)
		}
		return
	}

	d.pos++
	if d.pos-d.length > screenHeight {
		d.pos = -d.length
		d.length = randGen.Intn(12) + 8
		d.char = getRandomMatrixChar(randGen)

		pauseChance := 0.15 - (density * 0.05)
		if density > 1.0 {
			pauseChance = 0.05 - (density-1.0)*0.02
		}
		if pauseChance < 0.01 {
			pauseChance = 0.01
		}
		if randGen.Float64() < pauseChance {
			d.active = false
		}
	}
}

// draw places the drop's characters on the screen buffer.
func (d *Drop) draw(s *Screen, col int, trailColors []Color) {
	if !d.active {
		return
	}

	tailPos := d.pos - d.length
	for row := tailPos; row <= d.pos; row++ {
		if row >= 0 && row < s.height {
			s.chars[row][col] = d.char
			s.isBackground[row][col] = false
			distFromHead := d.pos - row
			colorIndex := int(float64(distFromHead) / float64(d.length) * float64(len(trailColors)))
			if colorIndex >= len(trailColors) {
				colorIndex = len(trailColors) - 1
			}
			s.colors[row][col] = trailColors[colorIndex]
		}
	}
}

// Screen manages the terminal display buffer.
type Screen struct {
	chars        [][]rune
	colors       [][]Color
	isBackground [][]bool
	height       int
	width        int
	mu           sync.RWMutex
}

// NewScreen creates a new Screen instance.
func NewScreen(height, width int) *Screen {
	s := &Screen{
		height:       height,
		width:        width,
		chars:        make([][]rune, height),
		colors:       make([][]Color, height),
		isBackground: make([][]bool, height),
	}
	for i := 0; i < height; i++ {
		s.chars[i] = make([]rune, width)
		s.colors[i] = make([]Color, width)
		s.isBackground[i] = make([]bool, width)
	}
	return s
}

// deepCopy creates a full, independent copy of the Screen's data.
func (s *Screen) deepCopy(target *Screen) {
	target.mu.Lock()
	defer target.mu.Unlock()

	target.height = s.height
	target.width = s.width
	target.chars = make([][]rune, s.height)
	target.colors = make([][]Color, s.height)
	target.isBackground = make([][]bool, s.height)

	for i := 0; i < s.height; i++ {
		target.chars[i] = make([]rune, s.width)
		target.colors[i] = make([]Color, s.width)
		target.isBackground[i] = make([]bool, s.width)
		copy(target.chars[i], s.chars[i])
		copy(target.colors[i], s.colors[i])
		copy(target.isBackground[i], s.isBackground[i])
	}
}

// checkAndResize checks for terminal size changes and resizes the screen buffer.
func (s *Screen) checkAndResize() bool {
	newHeight, newWidth := getTerminalSize()
	if newHeight == s.height && newWidth == s.width {
		return false
	}
	s.resize(newHeight, newWidth)
	return true
}

// resize updates the screen buffer dimensions.
func (s *Screen) resize(newHeight, newWidth int) {
	s.mu.Lock()
	defer s.mu.Unlock()

	newChars := make([][]rune, newHeight)
	newColors := make([][]Color, newHeight)
	newIsBackground := make([][]bool, newHeight)

	minHeight := min(s.height, newHeight)
	minWidth := min(s.width, newWidth)

	for i := 0; i < newHeight; i++ {
		newChars[i] = make([]rune, newWidth)
		newColors[i] = make([]Color, newWidth)
		newIsBackground[i] = make([]bool, newWidth)
		for j := 0; j < newWidth; j++ {
			if i < minHeight && j < minWidth {
				newChars[i][j] = s.chars[i][j]
				newColors[i][j] = s.colors[i][j]
				newIsBackground[i][j] = s.isBackground[i][j]
			} else {
				newChars[i][j] = ' '
				newColors[i][j] = Color{}
				newIsBackground[i][j] = true
			}
		}
	}

	s.chars = newChars
	s.colors = newColors
	s.isBackground = newIsBackground
	s.height, s.width = newHeight, newWidth
}

// clear resets the screen buffer to the background state.
func (s *Screen) clear() {
	s.mu.Lock()
	defer s.mu.Unlock()
	for i := 0; i < s.height; i++ {
		for j := 0; j < s.width; j++ {
			s.chars[i][j] = ' '
			s.isBackground[i][j] = true
		}
	}
}

// renderChanges outputs only the changed parts of the screen buffer to the terminal.
func (s *Screen) renderChanges(previous *Screen) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if previous.height != s.height || previous.width != s.width {
		s.renderFull() // Force full redraw on resize
		return
	}

	var builder strings.Builder
	var currentColor Color
	colorSet := false
	dirty := false

	// Iterate through the screen to find changes
	for row := 0; row < s.height; row++ {
		for col := 0; col < s.width; col++ {
			// Check if the character or color has changed
			if s.chars[row][col] != previous.chars[row][col] || s.colors[row][col] != previous.colors[row][col] {
				dirty = true
				// Move cursor to the changed position
				builder.WriteString(fmt.Sprintf("\x1b[%d;%dH", row+1, col+1))

				// Apply new color if necessary
				if s.isBackground[row][col] {
					if colorSet {
						builder.WriteString("\x1b[0m")
						colorSet = false
					}
				} else {
					if !colorSet || s.colors[row][col] != currentColor {
						builder.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", s.colors[row][col].R, s.colors[row][col].G, s.colors[row][col].B))
						currentColor = s.colors[row][col]
						colorSet = true
					}
				}
				builder.WriteRune(s.chars[row][col])
			}
		}
	}

	if dirty {
		builder.WriteString("\x1b[0m") // Reset colors at the end
		io.WriteString(os.Stdout, builder.String())
	}
}

// renderFull outputs the entire screen buffer to the terminal.
func (s *Screen) renderFull() {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var builder strings.Builder
	builder.WriteString("\x1b[H") // Move cursor to top-left

	var currentColor Color
	colorSet := false

	for row := 0; row < s.height; row++ {
		for col := 0; col < s.width; col++ {
			if s.isBackground[row][col] {
				if colorSet {
					builder.WriteString("\x1b[0m")
					colorSet = false
				}
			} else {
				if !colorSet || s.colors[row][col] != currentColor {
					builder.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", s.colors[row][col].R, s.colors[row][col].G, s.colors[row][col].B))
					currentColor = s.colors[row][col]
					colorSet = true
				}
			}
			builder.WriteRune(s.chars[row][col])
		}
		if row < s.height-1 {
			builder.WriteString("\r\n")
		}
	}
	builder.WriteString("\x1b[0m")
	io.WriteString(os.Stdout, builder.String())
}

// Color calculation functions
var colorThemes = map[string]Color{
	"green":  {0, 255, 0},
	"amber":  {255, 191, 0},
	"red":    {255, 0, 0},
	"orange": {255, 165, 0},
	"blue":   {0, 150, 255},
	"purple": {128, 0, 255},
	"cyan":   {0, 255, 255},
	"pink":   {255, 20, 147},
	"white":  {255, 255, 255},
}

func brightenColor(c Color, factor float64) Color {
	r := float64(c.R) * factor
	g := float64(c.G) * factor
	b := float64(c.B) * factor
	return Color{
		uint8(min(r, 255)),
		uint8(min(g, 255)),
		uint8(min(b, 255)),
	}
}

func dimColor(c Color, factor float64) Color {
	return Color{
		uint8(float64(c.R) * factor),
		uint8(float64(c.G) * factor),
		uint8(float64(c.B) * factor),
	}
}

// Terminal management
func getTerminalSize() (height, width int) {
	var sz struct{ rows, cols, x, y uint16 }
	syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&sz)),
	)
	return int(sz.rows), int(sz.cols)
}

func setupTerminal() {
	fmt.Print("\x1b[?1049h\x1b[?25l")
}

func restoreTerminal() {
	fmt.Print("\x1b[?25h\x1b[?1049l")
}

// Utility functions
func getRandomMatrixChar(randGen *rand.Rand) rune {
	return matrixRunes[randGen.Intn(len(matrixRunes))]
}

func min[T int | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

func main() {
	// 1. Configuration & Input Validation
	cfg := parseFlags()

	baseColor, exists := colorThemes[strings.ToLower(cfg.ColorName)]
	if !exists {
		fmt.Printf("Error: Unknown color '%s'\n", cfg.ColorName)
		os.Exit(1)
	}

	if cfg.Speed < 10 || cfg.Speed > 500 {
		fmt.Printf("Error: Speed must be between 10-500 milliseconds (got %d)\n", cfg.Speed)
		os.Exit(1)
	}

	if cfg.Density < 0.1 || cfg.Density > 3.0 {
		fmt.Printf("Error: Density must be between 0.1-3.0 (got %.1f)\n", cfg.Density)
		os.Exit(1)
	}

	// Set character set
	inputCharSet := strings.ToLower(cfg.CharSetString)
	if selectedChars, exists := matrixCharSets[inputCharSet]; exists {
		matrixRunes = selectedChars
	} else {
		matrixRunes = []rune(cfg.CharSetString)
	}

	if len(matrixRunes) == 0 {
		fmt.Printf("Error: Character set string cannot be empty\n")
		os.Exit(1)
	}

	// 2. Setup (Wiring Dependencies)
	randGen := rand.New(rand.NewSource(time.Now().UnixNano()))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	height, width := getTerminalSize()
	setupTerminal()
	defer restoreTerminal()

	const targetFPS = 10
	frameDuration := time.Second / targetFPS

	// Create the DropFactory and inject it into the MatrixEngine
	dropFactory := NewRandomDropFactory(randGen, height)
	engine := NewMatrixEngine(ctx, height, width, baseColor, cfg.Density, randGen, dropFactory)
	previousScreen := NewScreen(height, width)

	// Initial render
	engine.screen.renderFull()
	engine.screen.deepCopy(previousScreen)

	lastFrameTime := time.Now()

	// 3. Main Loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			now := time.Now()
			elapsed := now.Sub(lastFrameTime)
			engine.updateAndRender(previousScreen)
			timeToSleep := frameDuration - elapsed
			if timeToSleep > 0 {
				time.Sleep(timeToSleep)
			}
			lastFrameTime = now
		}
	}
}

// Configuration struct to hold parsed flags
type Config struct {
	ColorName     string
	Speed         int
	Density       float64
	ListOptions   bool
	CharSetString string
}

// Parses and validates command-line flags.
func parseFlags() *Config {
	cfg := &Config{}

	flag.StringVar(&cfg.ColorName, "color", "green", "Color theme (green, amber, red, orange, blue, purple, cyan, pink, white)")
	flag.IntVar(&cfg.Speed, "speed", 120, "Animation speed in milliseconds (10-500, lower = faster)")
	flag.Float64Var(&cfg.Density, "density", 0.7, "Drop density (0.1-3.0, higher = more drops)")
	flag.BoolVar(&cfg.ListOptions, "list", false, "List available options")
	flag.StringVar(&cfg.CharSetString, "chars", "matrix", "Character set name (matrix, binary, symbols, emojis, kanji, greek) or a custom string.")
	flag.Parse()

	if cfg.ListOptions {
		fmt.Println("Available options:")
		fmt.Println("\nColors:")
		for name := range colorThemes {
			fmt.Printf("  %s\n", name)
		}
		fmt.Println("\nCharacter Sets:")
		for name := range matrixCharSets {
			fmt.Printf("  %s\n", name)
		}
		fmt.Println("\nSpeed: 10-500 milliseconds (lower = faster)")
		fmt.Println("  Examples: 10 (insane), 20 (hyper), 30 (ultra), 50 (very fast), 80 (fast), 120 (normal)")
		fmt.Println("\nDensity: 0.1-3.0 (higher = more drops)")
		fmt.Println("  Examples: 0.5 (light), 0.7 (normal), 1.0 (full), 1.5 (heavy), 2.0 (intense), 3.0 (maximum)")
		fmt.Println("\nCharacter Set: Provide a named set (e.g., --chars kanji) or a custom string (e.g., --chars \"012345\")")
		os.Exit(0)
	}

	return cfg
}
