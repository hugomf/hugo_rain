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
	"syscall"
	"time"
	"unsafe"
)

// === CONFIG ===

// Config holds the configuration for the Matrix rain animation.
type Config struct {
	BaseColor        Color   // Base color for falling characters
	FPS              int     // Frames per second for animation
	Density          float64 // Number of character drops per column
	CharSet          []rune  // Characters used in the animation
	MinDropLength    int     // Minimum length of a drop's trail
	MaxDropLength    int     // Maximum length of a drop's trail
	ReactivateChance float64 // Probability of reactivating an inactive drop
	PauseChance      float64 // Probability of pausing an active drop
}

// === CONFIG DATA ===

// ConfigData stores predefined color themes and character sets.
type ConfigData struct {
	ColorThemes map[string]Color
	CharSets    map[string][]rune
}

var defaultConfigData = ConfigData{
	ColorThemes: map[string]Color{
		"green":  {0, 255, 0},
		"amber":  {255, 191, 0},
		"red":    {255, 0, 0},
		"orange": {255, 165, 0},
		"blue":   {0, 150, 255},
		"purple": {128, 0, 255},
		"cyan":   {0, 255, 255},
		"pink":   {255, 20, 147},
		"white":  {255, 255, 255},
	},
	CharSets: map[string][]rune{
		"matrix":   []rune("λｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ"),
		"binary":   []rune("01"),
		"symbols":  []rune("!@#$%^&*()_+-=[]{}|;':\",./<>?"),
		"emojis":   []rune("😂😅😊🔥💯✨🚀🎉🌟🌈"),
		"kanji":    []rune("書道日本漢字文化侍"),
		"greek":    []rune("αβγδεζηθικλμνξοπρστυφχψω"),
		"cyrillic": []rune("абвгдежзийклмнопрстуфхцчшщъыьэюя"),
	},
}

// === CONFIG PARSER ===

// ConfigParser parses command-line flags into a Config.
type ConfigParser struct {
	configData ConfigData // Predefined themes and character sets
}

// NewConfigParser creates a new ConfigParser with the given ConfigData.
func NewConfigParser(configData ConfigData) *ConfigParser {
	return &ConfigParser{configData: configData}
}

// Parse processes command-line flags and returns a Config.
func (p *ConfigParser) Parse() (cfg *Config, err error) {
	var (
		colorName   string
		fps         int
		density     float64
		listOptions bool
		charSetName string
	)
	flag.StringVar(&colorName, "color", "green", "color theme (green, amber, red, etc.)")
	flag.IntVar(&fps, "fps", 10, "frames per second (1-60)")
	flag.Float64Var(&density, "density", 0.7, "drop density (0.1-3.0)")
	flag.BoolVar(&listOptions, "list", false, "list available options")
	flag.StringVar(&charSetName, "chars", "matrix", "character set name or custom string")
	flag.Parse()

	if listOptions {
		return nil, p.listOptions()
	}

	baseColor, ok := p.configData.ColorThemes[strings.ToLower(colorName)]
	if !ok {
		return nil, fmt.Errorf("unknown color theme: %s", colorName)
	}
	if fps < 1 || fps > 60 {
		return nil, fmt.Errorf("fps out of range (1-60): got %d", fps)
	}
	if density < 0.1 || density > 3.0 {
		return nil, fmt.Errorf("density out of range (0.1-3.0): got %.1f", density)
	}

	charSet, err := p.resolveCharSet(charSetName)
	if err != nil {
		return nil, err
	}

	return &Config{
		BaseColor:        baseColor,
		FPS:              fps,
		Density:          density,
		CharSet:          charSet,
		MinDropLength:    8,
		MaxDropLength:    20,
		ReactivateChance: 0.01,
		PauseChance:      0.1,
	}, nil
}

// listOptions prints available options and returns an error to signal exit.
func (p *ConfigParser) listOptions() error {
	fmt.Println("Available options:")
	fmt.Println("Colors:")
	for name := range p.configData.ColorThemes {
		fmt.Println("  ", name)
	}
	fmt.Println("\nCharacter Sets:")
	for name := range p.configData.CharSets {
		fmt.Println("  ", name)
	}
	fmt.Println("\nFPS: 1-60")
	fmt.Println("Density: 0.1-3.0")
	return errors.New("list options requested")
}

// resolveCharSet converts a character set name or string to a rune slice.
func (p *ConfigParser) resolveCharSet(name string) ([]rune, error) {
	if set, ok := p.configData.CharSets[strings.ToLower(name)]; ok {
		return set, nil
	}
	if name == "" {
		return nil, errors.New("character set cannot be empty")
	}
	return []rune(name), nil
}

// === COLOR ===

// Color represents an RGB color value for terminal output.
type Color struct{ R, G, B uint8 }

// brighten increases the brightness of a color by a factor.
func brighten(c Color, factor float64) Color {
	return Color{
		R: uint8(clamp(255, float64(c.R)*factor)),
		G: uint8(clamp(255, float64(c.G)*factor)),
		B: uint8(clamp(255, float64(c.B)*factor)),
	}
}

// dim reduces the brightness of a color by a factor.
func dim(c Color, factor float64) Color {
	return Color{
		R: uint8(float64(c.R) * factor),
		G: uint8(float64(c.G) * factor),
		B: uint8(float64(c.B) * factor),
	}
}

// === TERMINAL INTERFACE ===

// Terminal defines operations for interacting with the terminal.
type Terminal interface {
	Setup()                         // Initialize terminal settings
	Restore()                       // Restore terminal to original state
	GetSize() (h, w int, err error) // Get terminal dimensions
}

// StdTerminal implements Terminal for standard terminal operations.
type StdTerminal struct{}

// Setup configures the terminal for animation (alternate buffer, hide cursor).
func (t *StdTerminal) Setup() {
	fmt.Print("\x1b[?1049h\x1b[?25l")
}

// Restore resets the terminal to its original state.
func (t *StdTerminal) Restore() {
	fmt.Print("\x1b[?25h\x1b[?1049l")
}

// GetSize returns the terminal's height and width in characters.
func (t *StdTerminal) GetSize() (h, w int, err error) {
	var sz struct{ rows, cols, x, y uint16 }
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&sz)))
	if errno != 0 {
		return 0, 0, fmt.Errorf("failed to get terminal size: %w", syscall.Errno(errno))
	}
	if sz.rows <= 0 || sz.cols <= 0 {
		return 0, 0, errors.New("invalid terminal dimensions")
	}
	return int(sz.rows), int(sz.cols), nil
}

// === DROP LOGIC ===

// Drop represents a single falling character in the Matrix rain.
type Drop struct {
	Pos    int  // Current vertical position
	Length int  // Length of the drop's trail
	Char   rune // Character to display
	Active bool // Whether the drop is currently falling
}

// NewDrop creates a new Drop with random initial state.
func NewDrop(height, minLength, maxLength int, charSet []rune, random *rand.Rand) (*Drop, error) {
	if len(charSet) == 0 {
		return nil, errors.New("character set cannot be empty")
	}
	return &Drop{
		Pos:    random.Intn(height) - random.Intn(height/2),
		Length: random.Intn(maxLength-minLength+1) + minLength,
		Char:   charSet[random.Intn(len(charSet))],
		Active: true,
	}, nil
}

// === ENGINE ===

// Engine manages the Matrix rain effect, coordinating drops and frames.
type Engine struct {
	height, width    int
	baseColor        Color
	trailColors      []Color
	drops            [][]*Drop
	random           *rand.Rand
	charSet          []rune
	density          float64
	terminal         Terminal
	frameBuffer      *Frame
	fps              int
	minDropLength    int
	maxDropLength    int
	reactivateChance float64
	pauseChance      float64
}

// NewEngine creates a new Engine with the given configuration.
func NewEngine(cfg *Config, random *rand.Rand, terminal Terminal) (*Engine, error) {
	if len(cfg.CharSet) == 0 {
		return nil, errors.New("character set cannot be empty")
	}
	if cfg.MinDropLength <= 0 || cfg.MaxDropLength < cfg.MinDropLength {
		return nil, errors.New("invalid drop length configuration")
	}
	if cfg.ReactivateChance < 0 || cfg.PauseChance < 0 {
		return nil, errors.New("invalid probability configuration")
	}
	e := &Engine{
		height:           0,
		width:            0,
		baseColor:        cfg.BaseColor,
		density:          cfg.Density,
		random:           random,
		charSet:          cfg.CharSet,
		terminal:         terminal,
		frameBuffer:      nil,
		fps:              cfg.FPS,
		minDropLength:    cfg.MinDropLength,
		maxDropLength:    cfg.MaxDropLength,
		reactivateChance: cfg.ReactivateChance,
		pauseChance:      cfg.PauseChance,
	}
	e.trailColors = e.calcTrailColors(5)
	return e, nil
}

// Update advances a drop's state based on terminal height.
func (e *Engine) Update(d *Drop, height int) {
	if !d.Active {
		if e.random.Float64() < e.reactivateChance*e.density {
			d.Active = true
			d.Pos = 0
			d.Length = e.random.Intn(e.maxDropLength-e.minDropLength+1) + e.minDropLength
			d.Char = e.charSet[e.random.Intn(len(e.charSet))]
		}
		return
	}
	d.Pos++
	if d.Pos-d.Length > height {
		d.Pos = -d.Length
		d.Length = e.random.Intn(e.maxDropLength-e.minDropLength+1) + e.minDropLength
		d.Char = e.charSet[e.random.Intn(len(e.charSet))]
		if e.random.Float64() < e.pauseChance {
			d.Active = false
		}
	}
}

// Resize adjusts the engine's dimensions and drop grid.
func (e *Engine) Resize(height, width int) error {
	if height == e.height && width == e.width {
		return nil
	}
	e.height, e.width = height, width

	e.drops = make([][]*Drop, width)
	for col := 0; col < width; col++ {
		numDrops := int(e.density + 0.5)
		if numDrops < 1 {
			numDrops = 1
		}
		e.drops[col] = make([]*Drop, numDrops)
		for i := 0; i < numDrops; i++ {
			drop, err := NewDrop(e.height, e.minDropLength, e.maxDropLength, e.charSet, e.random)
			if err != nil {
				return err
			}
			e.drops[col][i] = drop
		}
	}
	e.frameBuffer = NewFrame(e.height, e.width)
	return nil
}

// calcTrailColors generates a gradient of trail colors.
func (e *Engine) calcTrailColors(steps int) []Color {
	colors := make([]Color, steps)
	for i := 0; i < steps; i++ {
		fade := 1.0 - float64(i)/float64(steps)*0.8
		colors[i] = dim(e.baseColor, fade)
	}
	return colors
}

// NextFrame generates the next animation frame.
func (e *Engine) NextFrame() (*Frame, error) {
	if h, w, err := e.terminal.GetSize(); err == nil && (h != e.height || w != e.width) {
		if err := e.Resize(h, w); err != nil {
			return nil, err
		}
	}

	e.frameBuffer.clear()
	for col, drops := range e.drops {
		for _, drop := range drops {
			if drop == nil || !drop.Active {
				continue
			}
			e.Update(drop, e.height)
			if drop.Active {
				e.drawDrop(drop, e.frameBuffer, col)
			}
		}
	}
	return e.frameBuffer, nil
}

// getTrailColorIndex calculates the color index for a drop's trail position.
func (e *Engine) getTrailColorIndex(pos, tail, length int) int {
	dist := pos - tail
	idx := int(float64(dist) / float64(length) * float64(len(e.trailColors)))
	if idx >= len(e.trailColors) {
		return len(e.trailColors) - 1
	}
	return idx
}

// drawDrop renders a drop onto the frame with trail colors.
func (e *Engine) drawDrop(drop *Drop, frame *Frame, col int) {
	tail := drop.Pos - drop.Length
	startRow := max(tail, 0)
	endRow := min(drop.Pos, frame.height-1)
	for row := startRow; row <= endRow; row++ {
		frame.characters[row][col] = drop.Char
		frame.isBackground[row][col] = false
		frame.colors[row][col] = e.trailColors[e.getTrailColorIndex(drop.Pos, row, drop.Length)]
	}
}

// === FRAME ===

// Frame represents the in-memory terminal screen state.
type Frame struct {
	characters   [][]rune  // Characters to display
	colors       [][]Color // Colors for each position
	isBackground [][]bool  // Whether a position is background
	height       int
	width        int
}

// NewFrame creates a new Frame with the given dimensions.
func NewFrame(height, width int) *Frame {
	characters := make([][]rune, height)
	colors := make([][]Color, height)
	isBackground := make([][]bool, height)
	for i := range characters {
		characters[i] = make([]rune, width)
		colors[i] = make([]Color, width)
		isBackground[i] = make([]bool, width)
		for j := range characters[i] {
			characters[i][j] = ' '
			isBackground[i][j] = true
		}
	}
	return &Frame{
		height:       height,
		width:        width,
		characters:   characters,
		colors:       colors,
		isBackground: isBackground,
	}
}

// clear resets the frame to its default state.
func (f *Frame) clear() {
	for i := range f.characters {
		for j := range f.characters[i] {
			f.characters[i][j] = ' '
			f.isBackground[i][j] = true
			f.colors[i][j] = Color{} // Reset color to zero value
		}
	}
}

// === RENDERER ===

// Screen handles rendering frames to the terminal.
type Screen struct {
	out           io.Writer
	previousFrame *Frame
}

// NewScreen creates a new Screen with the given output writer.
func NewScreen(out io.Writer) *Screen {
	return &Screen{out: out}
}

// Draw renders a frame to the terminal, using delta rendering when possible.
func (s *Screen) Draw(frame *Frame) {
	if s.previousFrame == nil || s.previousFrame.height != frame.height || s.previousFrame.width != frame.width {
		s.fullRender(frame)
		s.previousFrame = NewFrame(frame.height, frame.width)
		s.copyFrame(frame, s.previousFrame)
	} else {
		s.deltaRender(frame)
		s.copyFrame(frame, s.previousFrame)
	}
}

// writeColor writes ANSI color codes to the builder if needed.
func (s *Screen) writeColor(b *strings.Builder, c Color, isColorSet *bool, currentColor *Color) bool {
	if !*isColorSet || c != *currentColor {
		b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B))
		*currentColor = c
		*isColorSet = true
		return true
	}
	return false
}

// fullRender draws the entire frame to the terminal.
func (s *Screen) fullRender(frame *Frame) {
	var b strings.Builder
	// Estimate: 1 rune + up to 20 bytes for color codes per cell, plus newlines
	b.Grow(frame.height * (frame.width*21 + 2))
	b.WriteString("\x1b[H") // Move cursor to top-left
	var currentColor Color
	isColorSet := false

	for row := 0; row < frame.height; row++ {
		for col := 0; col < frame.width; col++ {
			if frame.isBackground[row][col] {
				if isColorSet {
					b.WriteString("\x1b[0m") // Reset color
					isColorSet = false
				}
			} else if col == 0 || frame.colors[row][col] != frame.colors[row][col-1] {
				s.writeColor(&b, frame.colors[row][col], &isColorSet, &currentColor)
			}
			b.WriteRune(frame.characters[row][col])
		}
		if row < frame.height-1 {
			b.WriteString("\r\n")
		}
	}
	if isColorSet {
		b.WriteString("\x1b[0m") // Reset color at end
	}
	s.out.Write([]byte(b.String()))
}

// deltaRender draws only changed parts of the frame.
func (s *Screen) deltaRender(frame *Frame) {
	var b strings.Builder
	// Estimate: fewer cells change, so use a smaller initial size
	b.Grow(frame.height * frame.width * 10)
	var currentColor Color
	isColorSet := false
	hasChanges := false

	for col := 0; col < frame.width; col++ {
		for row := 0; row < frame.height; row++ {
			if frame.characters[row][col] != s.previousFrame.characters[row][col] || frame.colors[row][col] != s.previousFrame.colors[row][col] {
				hasChanges = true
				b.WriteString(fmt.Sprintf("\x1b[%d;%dH", row+1, col+1))
				if frame.isBackground[row][col] {
					if isColorSet {
						b.WriteString("\x1b[0m")
						isColorSet = false
					}
				} else {
					s.writeColor(&b, frame.colors[row][col], &isColorSet, &currentColor)
				}
				b.WriteRune(frame.characters[row][col])
			}
		}
	}
	if hasChanges {
		if isColorSet {
			b.WriteString("\x1b[0m")
		}
		s.out.Write([]byte(b.String()))
	}
}

// copyFrame copies the source frame to the destination frame.
func (s *Screen) copyFrame(src, dst *Frame) {
	for r := range src.characters {
		copy(dst.characters[r], src.characters[r])
		copy(dst.colors[r], src.colors[r])
		copy(dst.isBackground[r], src.isBackground[r])
	}
}

// === UTILS ===

// clamp limits a float64 value to a maximum, used for color calculations.
func clamp(max, val float64) float64 {
	if val < max {
		return val
	}
	return max
}

// max returns the larger of two integers.
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}

// min returns the smaller of two integers.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// === MATRIX RAIN ===

// MatrixRain holds the components of the Matrix rain animation.
type MatrixRain struct {
	engine   *Engine
	screen   *Screen
	terminal Terminal
	ctx      context.Context
	stop     context.CancelFunc
}

// NewMatrixRain creates and configures the Matrix rain animation.
func NewMatrixRain(configData ConfigData, out io.Writer, random *rand.Rand) (*MatrixRain, error) {
	parser := NewConfigParser(configData)
	cfg, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	terminal := &StdTerminal{}
	height, width, err := terminal.GetSize()
	if err != nil {
		return nil, fmt.Errorf("cannot get terminal size: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	engine, err := NewEngine(cfg, random, terminal)
	if err != nil {
		return nil, fmt.Errorf("failed to create engine: %w", err)
	}
	if err := engine.Resize(height, width); err != nil {
		return nil, fmt.Errorf("failed to resize engine: %w", err)
	}
	screen := NewScreen(out)

	return &MatrixRain{
		engine:   engine,
		screen:   screen,
		terminal: terminal,
		ctx:      ctx,
		stop:     stop,
	}, nil
}

// Run starts the Matrix rain animation.
func (r *MatrixRain) Run() error {
	defer r.stop()
	defer r.terminal.Restore()

	r.terminal.Setup()

	frameDuration := time.Second / time.Duration(r.engine.fps)
	tick := time.NewTicker(frameDuration)
	defer tick.Stop()

	for {
		select {
		case <-r.ctx.Done():
			return nil
		case <-tick.C:
			frame, err := r.engine.NextFrame()
			if err != nil {
				return fmt.Errorf("failed to generate frame: %w", err)
			}
			r.screen.Draw(frame)
		}
	}
}

// === MAIN ===

func main() {
	random := rand.New(rand.NewSource(time.Now().UnixNano()))
	rain, err := NewMatrixRain(defaultConfigData, os.Stdout, random)
	if err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
	if err := rain.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "error:", err)
		os.Exit(1)
	}
}
