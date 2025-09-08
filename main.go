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

// ---------- CONFIG ----------

// Config holds the configuration for the Matrix rain effect.
type Config struct {
	BaseColor Color   // Base color for the drops
	FPS       int     // Frames per second for animation
	Density   float64 // Density of drops per column
	CharSet   []rune  // Characters to use for drops
}

// ---------- CONFIG DATA ----------

// ConfigData holds predefined color themes and character sets.
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

// ---------- CONFIG PARSER ----------

// ConfigParser handles parsing command-line flags into a Config.
type ConfigParser struct {
	configData ConfigData // Inject ConfigData instead of using global
}

func NewConfigParser(configData ConfigData) *ConfigParser {
	return &ConfigParser{configData: configData}
}

func (p *ConfigParser) Parse() (*Config, error) {
	var (
		colorName   string
		fps         int
		density     float64
		listOptions bool
		charSetFlag string
	)
	flag.StringVar(&colorName, "color", "green", "theme (green, amber, red, ...)")
	flag.IntVar(&fps, "fps", 10, "frames per second (1-60)")
	flag.Float64Var(&density, "density", 0.7, "drop density 0.1-3.0")
	flag.BoolVar(&listOptions, "list", false, "list available options")
	flag.StringVar(&charSetFlag, "chars", "matrix", "named set or custom string")
	flag.Parse()

	if listOptions {
		p.listAndExit()
	}

	baseColor, ok := p.configData.ColorThemes[strings.ToLower(colorName)]
	if !ok {
		return nil, fmt.Errorf("unknown color '%s'", colorName)
	}
	if fps < 1 || fps > 60 {
		return nil, fmt.Errorf("fps out of range 1-60 (got %d)", fps)
	}
	if density < 0.1 || density > 3.0 {
		return nil, fmt.Errorf("density out of range 0.1-3.0 (got %.1f)", density)
	}

	charSet, err := p.resolveCharSet(charSetFlag)
	if err != nil {
		return nil, err
	}

	return &Config{
		BaseColor: baseColor,
		FPS:       fps,
		Density:   density,
		CharSet:   charSet,
	}, nil
}

func (p *ConfigParser) listAndExit() {
	fmt.Println("Available options:")
	fmt.Println("Colors:")
	for n := range p.configData.ColorThemes {
		fmt.Println(" ", n)
	}
	fmt.Println("\nCharacter Sets:")
	for n := range p.configData.CharSets {
		fmt.Println(" ", n)
	}
	fmt.Println("\nFPS: 1-60")
	fmt.Println("Density: 0.1-3.0")
	os.Exit(0)
}

func (p *ConfigParser) resolveCharSet(flag string) ([]rune, error) {
	if set, ok := p.configData.CharSets[strings.ToLower(flag)]; ok {
		return set, nil
	}
	if flag == "" {
		return nil, errors.New("character set cannot be empty")
	}
	return []rune(flag), nil
}

// ---------- COLOR -----------

// Color represents an RGB color value.
type Color struct{ R, G, B uint8 }

func brighten(c Color, f float64) Color {
	return Color{
		R: uint8(min(255, float64(c.R)*f)),
		G: uint8(min(255, float64(c.G)*f)),
		B: uint8(min(255, float64(c.B)*f)),
	}
}

func dim(c Color, f float64) Color {
	return Color{
		R: uint8(float64(c.R) * f),
		G: uint8(float64(c.G) * f),
		B: uint8(float64(c.B) * f),
	}
}

// ---------- TERMINAL INTERFACE ----------

// Terminal defines operations for interacting with the terminal.
type Terminal interface {
	Setup()
	Restore()
	GetSize() (h, w int, err error)
}

// StdTerminal implements Terminal for standard terminal operations.
type StdTerminal struct{}

func (t *StdTerminal) Setup() {
	fmt.Print("\x1b[?1049h\x1b[?25l")
}

func (t *StdTerminal) Restore() {
	fmt.Print("\x1b[?25h\x1b[?1049l")
}

func (t *StdTerminal) GetSize() (h, w int, err error) {
	var sz struct{ rows, cols, x, y uint16 }
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL, uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ), uintptr(unsafe.Pointer(&sz)))
	if e != 0 {
		return 0, 0, e
	}
	return int(sz.rows), int(sz.cols), nil
}

// ---------- DROP LOGIC ----------

// Drop represents a single falling character in the Matrix rain.
type Drop struct {
	Pos    int  // Current vertical position
	Length int  // Length of the drop trail
	Char   rune // Character to display
	Active bool // Whether the drop is currently falling
}

// DropUpdater defines the interface for updating drop state.
type DropUpdater interface {
	Update(d *Drop, height int)
}

// ---------- ENGINE ----------

// Engine manages the Matrix rain effect, orchestrating drops and frames.
type Engine struct {
	height, width int
	baseColor     Color
	trailColors   []Color
	drops         [][]*Drop
	randGen       *rand.Rand
	charSet       []rune
	density       float64
	term          Terminal
	frameBuffer   *Frame
	FPS           int // Added for access in App
}

// NewEngine creates a new Engine with the given configuration.
func NewEngine(cfg *Config, r *rand.Rand, term Terminal) *Engine {
	e := &Engine{
		height:      0,
		width:       0,
		baseColor:   cfg.BaseColor,
		density:     cfg.Density,
		randGen:     r,
		charSet:     cfg.CharSet,
		term:        term,
		frameBuffer: nil,
		FPS:         cfg.FPS, // Initialize FPS
	}
	e.trailColors = e.calcTrailColors(6)
	return e
}

// NewDrop creates a new Drop with random initial state.
func (e *Engine) NewDrop() *Drop {
	if len(e.charSet) == 0 {
		panic("charset cannot be empty")
	}
	return &Drop{
		Pos:    e.randGen.Intn(e.height) - e.randGen.Intn(e.height/2),
		Length: e.randGen.Intn(12) + 8,
		Char:   e.charSet[e.randGen.Intn(len(e.charSet))],
		Active: true,
	}
}

// Update implements DropUpdater to update a Drop's state.
func (e *Engine) Update(d *Drop, height int) {
	if !d.Active {
		chance := 0.005 * e.density
		if e.density > 1.0 {
			chance = 0.005 + (e.density-1.0)*0.02
		}
		if e.randGen.Float64() < chance {
			d.Active = true
			d.Pos = 0
			d.Length = e.randGen.Intn(12) + 8
			d.Char = e.charSet[e.randGen.Intn(len(e.charSet))]
		}
		return
	}
	d.Pos++
	if d.Pos-d.Length > height {
		d.Pos = -d.Length
		d.Length = e.randGen.Intn(12) + 8
		d.Char = e.charSet[e.randGen.Intn(len(e.charSet))]
		pause := 0.15 - e.density*0.05
		if e.density > 1.0 {
			pause = 0.05 - (e.density-1.0)*0.02
		}
		if pause < 0.01 {
			pause = 0.01
		}
		if e.randGen.Float64() < pause {
			d.Active = false
		}
	}
}

// Resize adjusts the engine's dimensions and drop grid.
func (e *Engine) Resize(h, w int) {
	if h == e.height && w == e.width {
		return
	}
	e.height, e.width = h, w

	e.drops = make([][]*Drop, w)
	for col := 0; col < w; col++ {
		numDrops := int(e.density)
		if e.randGen.Float64() < e.density-float64(numDrops) {
			numDrops++
		}
		if numDrops < 1 {
			numDrops = 1
		}

		e.drops[col] = make([]*Drop, numDrops)
		for i := 0; i < numDrops; i++ {
			e.drops[col][i] = e.NewDrop()
		}
	}
	e.frameBuffer = NewFrame(h, w)
}

func (e *Engine) calcTrailColors(steps int) []Color {
	c := make([]Color, steps)
	c[0] = brighten(e.baseColor, 1.2)
	for i := 1; i < steps; i++ {
		fade := 1.0 - (float64(i)/float64(steps-1))*0.7
		c[i] = dim(e.baseColor, fade)
	}
	return c
}

// NextFrame generates the next frame of the animation.
func (e *Engine) NextFrame() *Frame {
	if h, w, err := e.term.GetSize(); err == nil && (h != e.height || w != e.width) {
		e.Resize(h, w)
	}

	e.frameBuffer.clear()
	for col, drops := range e.drops {
		for _, drop := range drops {
			if drop == nil {
				continue
			}
			e.Update(drop, e.height)
			e.drawDrop(drop, e.frameBuffer, col)
		}
	}
	return e.frameBuffer
}

// drawDrop renders a drop onto the frame with trail colors.
func (e *Engine) drawDrop(drop *Drop, f *Frame, col int) {
	if !drop.Active {
		return
	}
	tail := drop.Pos - drop.Length
	for row := tail; row <= drop.Pos; row++ {
		if row >= 0 && row < f.height {
			f.chars[row][col] = drop.Char
			f.isBg[row][col] = false
			dist := drop.Pos - row
			idx := int(float64(dist) / float64(drop.Length) * float64(len(e.trailColors)))
			if idx >= len(e.trailColors) {
				idx = len(e.trailColors) - 1
			}
			f.colors[row][col] = e.trailColors[idx]
		}
	}
}

// ---------- FRAME -----------

// Frame represents the in-memory terminal screen state.
type Frame struct {
	chars  [][]rune
	colors [][]Color
	isBg   [][]bool
	height int
	width  int
}

func NewFrame(h, w int) *Frame {
	f := &Frame{
		height: h,
		width:  w,
		chars:  make([][]rune, h),
		colors: make([][]Color, h),
		isBg:   make([][]bool, h),
	}
	for i := 0; i < h; i++ {
		f.chars[i] = make([]rune, w)
		f.colors[i] = make([]Color, w)
		f.isBg[i] = make([]bool, w)
	}
	return f
}

func (f *Frame) clear() {
	for i := 0; i < f.height; i++ {
		for j := 0; j < f.width; j++ {
			f.chars[i][j] = ' '
			f.isBg[i][j] = true
		}
	}
}

// ---------- RENDERER --------

// Screen handles rendering frames to the terminal.
type Screen struct {
	out           io.Writer
	mu            sync.Mutex
	previousFrame *Frame
}

func NewScreen(out io.Writer) *Screen {
	return &Screen{out: out}
}

func (s *Screen) Draw(f *Frame) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.previousFrame == nil || s.previousFrame.height != f.height || s.previousFrame.width != f.width {
		s.fullRender(f)
		s.previousFrame = NewFrame(f.height, f.width)
		s.copyFrame(f, s.previousFrame)
	} else {
		s.deltaRender(f)
		s.copyFrame(f, s.previousFrame)
	}
}

func (s *Screen) fullRender(f *Frame) {
	var b strings.Builder
	b.Grow(f.height*f.width*12 + 10)
	b.WriteString("\x1b[H")
	var cur Color
	set := false
	for row := 0; row < f.height; row++ {
		for col := 0; col < f.width; col++ {
			if f.isBg[row][col] {
				if set {
					b.WriteString("\x1b[0m")
					set = false
				}
			} else {
				if !set || f.colors[row][col] != cur {
					c := f.colors[row][col]
					b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B))
					cur = c
					set = true
				}
			}
			b.WriteRune(f.chars[row][col])
		}
		if row < f.height-1 {
			b.WriteString("\r\n")
		}
	}
	if set {
		b.WriteString("\x1b[0m")
	}
	s.out.Write([]byte(b.String()))
}

func (s *Screen) deltaRender(f *Frame) {
	var b strings.Builder
	b.Grow(f.height * f.width * 12)
	var cur Color
	set := false
	dirty := false
	for row := 0; row < f.height; row++ {
		for col := 0; col < f.width; col++ {
			if f.chars[row][col] != s.previousFrame.chars[row][col] || f.colors[row][col] != s.previousFrame.colors[row][col] {
				dirty = true
				b.WriteString(fmt.Sprintf("\x1b[%d;%dH", row+1, col+1))
				if f.isBg[row][col] {
					if set {
						b.WriteString("\x1b[0m")
						set = false
					}
				} else {
					if !set || f.colors[row][col] != cur {
						c := f.colors[row][col]
						b.WriteString(fmt.Sprintf("\x1b[38;2;%d;%d;%dm", c.R, c.G, c.B))
						cur = c
						set = true
					}
				}
				b.WriteRune(f.chars[row][col])
			}
		}
	}
	if dirty {
		if set {
			b.WriteString("\x1b[0m")
		}
		s.out.Write([]byte(b.String()))
	}
}

func (s *Screen) copyFrame(src, dst *Frame) {
	for r := 0; r < src.height; r++ {
		copy(dst.chars[r], src.chars[r])
		copy(dst.colors[r], src.colors[r])
		copy(dst.isBg[r], src.isBg[r])
	}
}

// ---------- UTILS -----------

func min[T int | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// ---------- APP FACTORY -----------

// App holds the components of the Matrix rain application.
type App struct {
	Engine *Engine
	Screen *Screen
	Term   Terminal
	Ctx    context.Context
	Stop   context.CancelFunc
}

// NewApp creates and configures the Matrix rain application.
func NewApp(configData ConfigData, out io.Writer, rng *rand.Rand) (*App, error) {
	parser := NewConfigParser(configData)
	cfg, err := parser.Parse()
	if err != nil {
		return nil, fmt.Errorf("failed to parse config: %w", err)
	}

	term := &StdTerminal{}
	h, w, err := term.GetSize()
	if err != nil || h <= 0 || w <= 0 {
		return nil, fmt.Errorf("cannot get terminal size: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)

	engine := NewEngine(cfg, rng, term)
	engine.Resize(h, w)
	screen := NewScreen(out)

	return &App{
		Engine: engine,
		Screen: screen,
		Term:   term,
		Ctx:    ctx,
		Stop:   stop,
	}, nil
}

// Run starts the Matrix rain animation.
func (a *App) Run() {
	defer a.Stop()
	defer a.Term.Restore()

	a.Term.Setup()

	frameDuration := time.Second / time.Duration(a.Engine.FPS)
	tick := time.NewTicker(frameDuration)
	defer tick.Stop()

	for {
		select {
		case <-a.Ctx.Done():
			return
		case <-tick.C:
			a.Screen.Draw(a.Engine.NextFrame())
		}
	}
}

// ---------- MAIN -----------

func main() {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	app, err := NewApp(defaultConfigData, os.Stdout, rng)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	app.Run()
}
