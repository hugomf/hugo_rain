package main

import (
	"context"
	"errors"
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
	height, width int
	drops         []Drop
	baseColor     Color
	trailColors   []Color
	density       float64
	animationCtx  context.Context
	randGen       *rand.Rand
	dropFactory   DropFactory
}

// NewMatrixEngine creates a new instance of the MatrixEngine.
func NewMatrixEngine(ctx context.Context, height, width int, baseColor Color, density float64, randGen *rand.Rand, dropFactory DropFactory) *MatrixEngine {
	engine := &MatrixEngine{
		height:       height,
		width:        width,
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
		drops[i] = me.dropFactory.CreateDrop(me.height)
	}
	return drops
}

// generateFrame computes the new frame of animation and returns a Frame object.
func (me *MatrixEngine) generateFrame() Frame {
	// First, check for terminal size changes and resize the engine's drops if needed.
	newHeight, newWidth, err := getTerminalSize()
	if err == nil && (newHeight != me.height || newWidth != me.width) {
		me.height, me.width = newHeight, newWidth
		me.drops = me.resizeDrops(me.width, me.height)
	}

	frame := NewFrame(me.height, me.width)

	dropIndex := 0
	for col := 0; col < me.width; col++ {
		dropsPerColumn := int(me.density)
		if me.randGen.Float64() < (me.density - float64(dropsPerColumn)) {
			dropsPerColumn++
		}
		if dropsPerColumn < 1 {
			dropsPerColumn = 1
		}

		for i := 0; i < dropsPerColumn; i++ {
			if dropIndex < len(me.drops) {
				me.drops[dropIndex].update(me.height, me.density, me.randGen)
				me.drops[dropIndex].draw(&frame, col, me.trailColors)
				dropIndex++
			}
		}
	}
	return frame
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
func (d *Drop) draw(f *Frame, col int, trailColors []Color) {
	if !d.active {
		return
	}

	tailPos := d.pos - d.length
	for row := tailPos; row <= d.pos; row++ {
		if row >= 0 && row < f.height {
			f.chars[row][col] = d.char
			f.isBackground[row][col] = false
			distFromHead := d.pos - row
			colorIndex := int(float64(distFromHead) / float64(d.length) * float64(len(trailColors)))
			if colorIndex >= len(trailColors) {
				colorIndex = len(trailColors) - 1
			}
			f.colors[row][col] = trailColors[colorIndex]
		}
	}
}

// Frame represents a single snapshot of the animation, decoupled from the display.
type Frame struct {
	chars        [][]rune
	colors       [][]Color
	isBackground [][]bool
	height       int
	width        int
}

// NewFrame creates a new Frame instance.
func NewFrame(height, width int) Frame {
	f := Frame{
		height:       height,
		width:        width,
		chars:        make([][]rune, height),
		colors:       make([][]Color, height),
		isBackground: make([][]bool, height),
	}
	for i := 0; i < height; i++ {
		f.chars[i] = make([]rune, width)
		f.colors[i] = make([]Color, width)
		f.isBackground[i] = make([]bool, width)
		for j := 0; j < width; j++ {
			f.isBackground[i][j] = true
		}
	}
	return f
}

// Screen manages the terminal display buffer.
type Screen struct {
	height, width int
	previousFrame Frame
	mu            sync.RWMutex
}

// NewScreen creates a new Screen instance.
func NewScreen(height, width int) *Screen {
	s := &Screen{
		height: height,
		width:  width,
	}
	s.previousFrame = NewFrame(height, width)
	return s
}

// renderFrame outputs only the changed parts of the frame to the terminal.
func (s *Screen) renderFrame(currentFrame Frame) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	// If the terminal size has changed, force a full render.
	if s.previousFrame.height != currentFrame.height || s.previousFrame.width != currentFrame.width {
		s.renderFull(currentFrame)
		s.previousFrame = currentFrame
		return
	}

	var builder strings.Builder
	var currentColor Color
	colorSet := false
	dirty := false

	for row := 0; row < currentFrame.height; row++ {
		for col := 0; col < currentFrame.width; col++ {
			// Check if the character or color has changed
			if currentFrame.chars[row][col] != s.previousFrame.chars[row][col] || currentFrame.colors[row][col] != s.previousFrame.colors[row][col] {
				dirty = true
				// Move cursor to the changed position
				builder.WriteString(fmt.Sprintf("\x1b[%d;%dH", row+1, col+1))

				if currentFrame.isBackground[row][col] {
					if colorSet {
						builder.WriteString("\x1b[0m")
						colorSet = false
					}
					builder.WriteRune(' ') // CRITICAL FIX: Add a space to clear the character
				} else {
					if !colorSet || currentFrame.colors[row][col] != currentColor {
						builder.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", currentFrame.colors[row][col].R, currentFrame.colors[row][col].G, currentFrame.colors[row][col].B))
						currentColor = currentFrame.colors[row][col]
						colorSet = true
					}
					builder.WriteRune(currentFrame.chars[row][col])
				}
			}
		}
	}

	if dirty {
		builder.WriteString("\x1b[0m") // Reset colors at the end
		io.WriteString(os.Stdout, builder.String())
	}
	s.previousFrame = currentFrame
}

// renderFull outputs the entire frame to the terminal.
func (s *Screen) renderFull(frame Frame) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	var builder strings.Builder
	builder.WriteString("\x1b[H") // Move cursor to top-left

	var currentColor Color
	colorSet := false

	for row := 0; row < frame.height; row++ {
		for col := 0; col < frame.width; col++ {
			if frame.isBackground[row][col] {
				if colorSet {
					builder.WriteString("\x1b[0m")
					colorSet = false
				}
			} else {
				if !colorSet || frame.colors[row][col] != currentColor {
					builder.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", frame.colors[row][col].R, frame.colors[row][col].G, frame.colors[row][col].B))
					currentColor = frame.colors[row][col]
					colorSet = true
				}
			}
			builder.WriteRune(frame.chars[row][col])
		}
		if row < frame.height-1 {
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
func getTerminalSize() (height, width int, err error) {
	var sz struct{ rows, cols, x, y uint16 }
	_, _, e := syscall.Syscall(
		syscall.SYS_IOCTL,
		uintptr(syscall.Stdout),
		uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&sz)),
	)
	if e != 0 {
		return 0, 0, e
	}
	return int(sz.rows), int(sz.cols), nil
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
	cfg, err := parseFlags()
	if err != nil {
		fmt.Printf("Error: %v\n", err)
		os.Exit(1)
	}

	// 2. Setup (Wiring Dependencies)
	randGen := rand.New(rand.NewSource(time.Now().UnixNano()))

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer cancel()

	height, width, err := getTerminalSize()
	if err != nil {
		fmt.Printf("Error: Could not get terminal size: %v\n", err)
		os.Exit(1)
	}
	if height <= 0 || width <= 0 {
		fmt.Println("Error: Terminal dimensions are too small or could not be determined.")
		os.Exit(1)
	}

	setupTerminal()
	defer restoreTerminal()

	frameDuration := time.Second / time.Duration(cfg.FPS)

	// Create the DropFactory and inject it into the MatrixEngine
	dropFactory := NewRandomDropFactory(randGen, height)
	engine := NewMatrixEngine(ctx, height, width, cfg.BaseColor, cfg.Density, randGen, dropFactory)
	screen := NewScreen(height, width)

	// Initial render
	initialFrame := engine.generateFrame()
	screen.renderFull(initialFrame)
	screen.previousFrame = initialFrame

	lastFrameTime := time.Now()

	// 3. Main Loop
	for {
		select {
		case <-ctx.Done():
			return
		default:
			now := time.Now()
			elapsed := now.Sub(lastFrameTime)

			// Generate the new frame and then render it.
			currentFrame := engine.generateFrame()
			screen.renderFrame(currentFrame)

			timeToSleep := frameDuration - elapsed
			if timeToSleep > 0 {
				time.Sleep(timeToSleep)
			}
			lastFrameTime = now
		}
	}
}

// Configuration struct to hold parsed flags and validated values.
type Config struct {
	BaseColor     Color
	FPS           int
	Density       float64
	ListOptions   bool
	CharSetString string
}

// Parses and validates command-line flags.
func parseFlags() (*Config, error) {
	cfg := &Config{}

	var colorName string
	var fps int

	flag.StringVar(&colorName, "color", "green", "Color theme (green, amber, red, orange, blue, purple, cyan, pink, white)")
	flag.IntVar(&fps, "fps", 10, "Animation speed in frames per second (1-60, higher = faster)")
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
		fmt.Println("\nFPS: 1-60 (higher = faster)")
		fmt.Println("  Examples: 1 (very slow), 10 (normal), 30 (fast), 60 (very fast)")
		fmt.Println("\nDensity: 0.1-3.0 (higher = more drops)")
		fmt.Println("  Examples: 0.5 (light), 0.7 (normal), 1.0 (full), 1.5 (heavy), 2.0 (intense), 3.0 (maximum)")
		fmt.Println("\nCharacter Set: Provide a named set (e.g., --chars kanji) or a custom string (e.g., --chars \"012345\")")
		os.Exit(0)
	}

	// Validate color
	baseColor, exists := colorThemes[strings.ToLower(colorName)]
	if !exists {
		return nil, errors.New(fmt.Sprintf("unknown color '%s'", colorName))
	}
	cfg.BaseColor = baseColor

	// Validate FPS
	if fps < 1 || fps > 60 {
		return nil, errors.New(fmt.Sprintf("fps must be between 1-60 (got %d)", fps))
	}
	cfg.FPS = fps

	// Validate density
	if cfg.Density < 0.1 || cfg.Density > 3.0 {
		return nil, errors.New(fmt.Sprintf("density must be between 0.1-3.0 (got %.1f)", cfg.Density))
	}

	// Set character set
	if selectedChars, exists := matrixCharSets[strings.ToLower(cfg.CharSetString)]; exists {
		matrixRunes = selectedChars
	} else {
		matrixRunes = []rune(cfg.CharSetString)
	}

	if len(matrixRunes) == 0 {
		return nil, errors.New("character set string cannot be empty")
	}

	return cfg, nil
}
