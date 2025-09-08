package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"io"
	"log"
	"math/rand"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

// === CONFIG ===

// Default configuration values for the animation.
const (
	defaultFPS              = 10
	defaultDensity          = 0.7
	defaultColor            = "green"
	defaultCharSet          = "matrix"
	defaultMinDropLength    = 8
	defaultMaxDropLength    = 20
	defaultReactivateChance = 0.01
	defaultPauseChance      = 0.1
)

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
	Debug            bool    // Enable debug logging
}

// validate checks the configuration for validity.
func (c *Config) validate() error {
	if len(c.CharSet) == 0 {
		return errors.New("character set cannot be empty")
	}
	if c.FPS < 1 || c.FPS > 60 {
		return fmt.Errorf("fps out of range (1-60): got %d", c.FPS)
	}
	if c.Density < 0.1 || c.Density > 3.0 {
		return fmt.Errorf("density out of range (0.1-3.0): got %.1f", c.Density)
	}
	if c.MinDropLength <= 0 || c.MaxDropLength < c.MinDropLength {
		return errors.New("invalid drop length configuration")
	}
	if c.ReactivateChance < 0 || c.PauseChance < 0 {
		return errors.New("invalid probability configuration")
	}
	return nil
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
		"kanji":    []rune("書道日本漢字文化侍忍者武士刀剣"),
		"greek":    []rune("αβγδεζηθικλμνξοπρστυφχψωΑΒΓΔΕΖΗΘΙΚΛΜΝΞΟΠΡΣΤΥΦΧΨΩ"),
		"cyrillic": []rune("абвгдежзийклмнопрстуфхцчшщъыьэюяАБВГДЕЖЗИЙКЛМНОПРСТУФХЦЧШЩЪЫЬЭЮЯ"),
		"persian":  []rune("ابتثجحخدذرزسشصضطظعغفقكلمنهويپچڈگھژکںیےآأؤإئءًٌٍَُِّْ"),
		"binary":   []rune("01"),
		"hex":      []rune("0123456789ABCDEF"),
		"symbols":  []rune("!@#$%^&*()_+-=[]{}|;':\",./<>?"),
		"emojis":   []rune("😂😅😊🔥✨🚀🎉🌟🌈💩👻💀☠️👽👾"),
		"hearts":   []rune("❤️🧡💛💚💙💜🤎🖤🤍"),
		"blocks":   []rune("◼️◻️🟥🟧🟨🟩🟦🟪⬛⬜🟫"),
		"circles":  []rune("🔴🟠🟡🟢🔵🟣⚫⚪🟤"),
		"mayan":    []rune("◊◈◉◎●○◐◑◒◓◔◕◖◗◘◙◚◛◜◝◞◟◠◡◢◣◤◥◦◧◨◩◪◫◬◭◮◯◰◱◲◳◴◵◶◷◸◹◺◻◼◽◾◿"),
		"aztec":    []rune("☀︎☽☾✦✧⋚⋛⋜⋝⋞⋟⋠⋡❦❧◿▲△▴▵▶▷▸▹►▻▼▽▾▿"),
		"dna":      []rune("ATCG"),
		"arrows":   []rune("←↑→↓↖↗↘↙⇐⇑⇒⇓"),
		"math":     []rune("∀∁∂∃∄∅∆∇∈∉∊∋∌∍∎∏∐∑−∓∔∕∖∗∘∙√∛∜∝∞∟∠∡∢∣∤∥∦∧∨∩∪"),
		"braille":  []rune("⠁⠂⠃⠄⠅⠆⠇⠈⠉⠊⠋⠌⠍⠎⠏⠐⠑⠒⠓⠔⠕⠖⠗⠘⠙⠚⠛⠜⠝⠞⠟⠠⠡⠢⠣⠤⠥⠦⠧⠨⠩⠪⠫⠬⠭⠮⠯"),
		"ascii":    []rune("ABCDEFGHIJKLMNOPQRSTUVWXYZabcdefghijklmnopqrstuvwxyz0123456789"),
		"minimal":  []rune(".*+"),
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
		debug       bool
	)
	flag.StringVar(&colorName, "color", defaultColor, "color theme (green, amber, red, etc.)")
	flag.IntVar(&fps, "fps", defaultFPS, "frames per second (1-60)")
	flag.Float64Var(&density, "density", defaultDensity, "drop density (0.1-3.0)")
	flag.BoolVar(&listOptions, "list", false, "list available options")
	flag.StringVar(&charSetName, "chars", defaultCharSet, "character set name or custom string")
	flag.BoolVar(&debug, "debug", false, "enable debug logging")
	flag.Parse()

	if listOptions {
		return nil, p.listOptions()
	}

	baseColor, ok := p.configData.ColorThemes[strings.ToLower(colorName)]
	if !ok {
		return nil, fmt.Errorf("unknown color theme: %s", colorName)
	}

	charSet, err := p.resolveCharSet(charSetName)
	if err != nil {
		return nil, err
	}

	cfg = &Config{
		BaseColor:        baseColor,
		FPS:              fps,
		Density:          density,
		CharSet:          charSet,
		MinDropLength:    defaultMinDropLength,
		MaxDropLength:    defaultMaxDropLength,
		ReactivateChance: defaultReactivateChance,
		PauseChance:      defaultPauseChance,
		Debug:            debug,
	}
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	return cfg, nil
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
	fmt.Println("Debug: enable with --debug")
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

// === TERMINAL ===

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
			f.colors[i][j] = Color{}
		}
	}
}

// === DROP ===

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

// === DROP MANAGER ===

// DropManager handles the creation and updating of drops.
type DropManager struct {
	drops            [][]*Drop
	height, width    int
	charSet          []rune
	minDropLength    int
	maxDropLength    int
	density          float64
	reactivateChance float64
	pauseChance      float64
	random           *rand.Rand
	debug            bool
}

// NewDropManager creates a new DropManager with the given configuration.
func NewDropManager(cfg *Config, random *rand.Rand) (*DropManager, error) {
	return &DropManager{
		drops:            nil,
		height:           0,
		width:            0,
		charSet:          cfg.CharSet,
		minDropLength:    cfg.MinDropLength,
		maxDropLength:    cfg.MaxDropLength,
		density:          cfg.Density,
		reactivateChance: cfg.ReactivateChance,
		pauseChance:      cfg.PauseChance,
		random:           random,
		debug:            cfg.Debug,
	}, nil
}

// Resize adjusts the drop grid to the new dimensions.
func (m *DropManager) Resize(height, width int) error {
	if height == m.height && width == m.width {
		return nil
	}
	m.height, m.width = height, width

	m.drops = make([][]*Drop, width)
	for col := 0; col < width; col++ {
		numDrops := int(m.density + 0.5)
		if numDrops < 1 {
			numDrops = 1
		}
		m.drops[col] = make([]*Drop, numDrops)
		for i := 0; i < numDrops; i++ {
			drop, err := NewDrop(m.height, m.minDropLength, m.maxDropLength, m.charSet, m.random)
			if err != nil {
				return err
			}
			m.drops[col][i] = drop
		}
	}
	if m.debug {
		log.Printf("Resized drop grid to %dx%d with %d total drops", height, width, width*int(m.density+0.5))
	}
	return nil
}

// Update advances a drop's state based on terminal height.
func (m *DropManager) Update(d *Drop) {
	if !d.Active {
		if m.random.Float64() < m.reactivateChance*m.density {
			d.Active = true
			d.Pos = 0
			d.Length = m.random.Intn(m.maxDropLength-m.minDropLength+1) + m.minDropLength
			d.Char = m.charSet[m.random.Intn(len(m.charSet))]
			if m.debug {
				log.Printf("Reactivated drop at pos %d with char %q", d.Pos, d.Char)
			}
		}
		return
	}
	d.Pos++
	if d.Pos-d.Length > m.height {
		d.Pos = -d.Length
		d.Length = m.random.Intn(m.maxDropLength-m.minDropLength+1) + m.minDropLength
		d.Char = m.charSet[m.random.Intn(len(m.charSet))]
		if m.random.Float64() < m.pauseChance {
			d.Active = false
			if m.debug {
				log.Printf("Paused drop at pos %d", d.Pos)
			}
		}
	}
}

// Drops returns the current drop grid.
func (m *DropManager) Drops() [][]*Drop {
	return m.drops
}

// === ENGINE ===

// Engine manages the Matrix rain effect, generating frames from drops.
type Engine struct {
	height, width int
	baseColor     Color
	trailColors   []Color
	manager       *DropManager
	terminal      Terminal
	frameBuffer   *Frame
	fps           int
	debug         bool
}

// NewEngine creates a new Engine with the given configuration.
func NewEngine(cfg *Config, random *rand.Rand, terminal Terminal) (*Engine, error) {
	if err := cfg.validate(); err != nil {
		return nil, err
	}
	manager, err := NewDropManager(cfg, random)
	if err != nil {
		return nil, fmt.Errorf("failed to create drop manager: %w", err)
	}
	e := &Engine{
		height:      0,
		width:       0,
		baseColor:   cfg.BaseColor,
		manager:     manager,
		terminal:    terminal,
		frameBuffer: nil,
		fps:         cfg.FPS,
		debug:       cfg.Debug,
	}
	e.trailColors = e.calcTrailColors(5)
	return e, nil
}

// calcTrailColors generates a gradient of trail colors.
// The steps parameter must be positive to create a valid gradient.
func (e *Engine) calcTrailColors(steps int) []Color {
	colors := make([]Color, steps)
	for i := 0; i < steps; i++ {
		fade := 1.0 - float64(i)/float64(steps)*0.8
		colors[i] = dim(e.baseColor, fade)
	}
	return colors
}

// Resize adjusts the engine's dimensions and frame buffer.
func (e *Engine) Resize(height, width int) error {
	if err := e.manager.Resize(height, width); err != nil {
		return err
	}
	e.height, e.width = height, width
	e.frameBuffer = NewFrame(height, width)
	return nil
}

// NextFrame generates the next animation frame.
func (e *Engine) NextFrame() (*Frame, error) {
	if h, w, err := e.terminal.GetSize(); err == nil && (h != e.height || w != e.width) {
		if err := e.Resize(h, w); err != nil {
			return nil, err
		}
	}

	e.frameBuffer.clear()
	drops := e.manager.Drops()
	for col, colDrops := range drops {
		for _, drop := range colDrops {
			if drop == nil || !drop.Active {
				continue
			}
			e.manager.Update(drop)
			if drop.Active {
				e.drawDrop(drop, e.frameBuffer, col)
			}
		}
	}
	if e.debug {
		log.Printf("Generated frame with %dx%d dimensions", e.height, e.width)
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

// === SCREEN ===

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

// === HELPERS ===

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

// === MAIN ===

func main() {
	log.SetFlags(log.Lshortfile | log.Ltime)
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
