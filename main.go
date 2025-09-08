// main.go
// Terminal Matrix rain – Final Version merging the best of both.
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

// ---------- CONFIG ----------------------------------------------------------

type Config struct {
	BaseColor Color
	FPS       int
	Density   float64
	CharSet   []rune
}

func ParseFlags() (*Config, error) {
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
		listAndExit()
	}

	baseColor, ok := colorThemes[strings.ToLower(colorName)]
	if !ok {
		return nil, fmt.Errorf("unknown color '%s'", colorName)
	}
	if fps < 1 || fps > 60 {
		return nil, fmt.Errorf("fps out of range 1-60 (got %d)", fps)
	}
	if density < 0.1 || density > 3.0 {
		return nil, fmt.Errorf("density out of range 0.1-3.0 (got %.1f)", density)
	}

	charSet, err := resolveCharSet(charSetFlag)
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

func listAndExit() {
	fmt.Println("Available options:")
	fmt.Println("Colors:")
	for n := range colorThemes {
		fmt.Println(" ", n)
	}
	fmt.Println("\nCharacter Sets:")
	for n := range matrixCharSets {
		fmt.Println(" ", n)
	}
	fmt.Println("\nFPS: 1-60")
	fmt.Println("Density: 0.1-3.0")
	os.Exit(0)
}

func resolveCharSet(flag string) ([]rune, error) {
	if set, ok := matrixCharSets[strings.ToLower(flag)]; ok {
		return set, nil
	}
	if flag == "" {
		return nil, errors.New("character set cannot be empty")
	}
	return []rune(flag), nil
}

// ---------- COLOR -----------------------------------------------------------

type Color struct{ R, G, B uint8 }

var colorThemes = map[string]Color{
	"green": {0, 255, 0}, "amber": {255, 191, 0}, "red": {255, 0, 0},
	"orange": {255, 165, 0}, "blue": {0, 150, 255}, "purple": {128, 0, 255},
	"cyan": {0, 255, 255}, "pink": {255, 20, 147}, "white": {255, 255, 255},
}

func brighten(c Color, f float64) Color {
	return Color{
		R: uint8(min(255, float64(c.R)*f)),
		G: uint8(min(255, float64(c.G)*f)),
		B: uint8(min(255, float64(c.B)*f)),
	}
}

func dim(c Color, f float64) Color {
	return Color{R: uint8(float64(c.R) * f), G: uint8(float64(c.G) * f), B: uint8(float64(c.B) * f)}
}

// ---------- TERMINAL --------------------------------------------------------

type TermSizeFunc func() (h, w int, err error)

func GetTermSize() (h, w int, err error) {
	var sz struct{ rows, cols, x, y uint16 }
	_, _, e := syscall.Syscall(syscall.SYS_IOCTL,
		uintptr(syscall.Stdout), uintptr(syscall.TIOCGWINSZ),
		uintptr(unsafe.Pointer(&sz)))
	if e != 0 {
		return 0, 0, e
	}
	return int(sz.rows), int(sz.cols), nil
}

func SetupTerminal()   { fmt.Print("\x1b[?1049h\x1b[?25l") }
func RestoreTerminal() { fmt.Print("\x1b[?25h\x1b[?1049l") }

// ---------- CHARACTER SETS --------------------------------------------------

var matrixCharSets = map[string][]rune{
	"matrix":   []rune("λｱｲｳｴｵｶｷｸｹｺｻｼｽｾｿﾀﾁﾂﾃﾄﾅﾆﾇﾈﾉﾊﾋﾌﾍﾎﾏﾐﾑﾒﾓﾔﾕﾖﾗﾘﾙﾚﾛﾜﾝ"),
	"binary":   []rune("01"),
	"symbols":  []rune("!@#$%^&*()_+-=[]{}|;':\",./<>?"),
	"emojis":   []rune("😂😅😊🔥💯✨🚀🎉🌟🌈"),
	"kanji":    []rune("書道日本漢字文化侍"),
	"greek":    []rune("αβγδεζηθικλμνξοπρστυφχψω"),
	"cyrillic": []rune("абвгдежзийклмнопрстуфхцчшщъыьэюя"),
}

// ---------- DROP LOGIC ------------------------------------------------------

type Drop struct {
	pos    int
	length int
	char   rune
	active bool
}

type DropFactory interface {
	CreateDrop(screenHeight int) Drop
}

type randomFactory struct {
	randGen *rand.Rand
	screenH int
	charSet []rune
}

func NewRandomFactory(r *rand.Rand, h int, set []rune) DropFactory {
	return &randomFactory{randGen: r, screenH: h, charSet: set}
}

func (f *randomFactory) CreateDrop(h int) Drop {
	return Drop{
		pos:    f.randGen.Intn(h) - f.randGen.Intn(h/2),
		length: f.randGen.Intn(12) + 8,
		char:   f.charSet[f.randGen.Intn(len(f.charSet))],
		active: true,
	}
}

// ---------- ENGINE ----------------------------------------------------------

type Engine struct {
	height, width int
	baseColor     Color
	trailColors   []Color
	density       float64
	drops         [][]Drop
	randGen       *rand.Rand
	factory       DropFactory
	sizeFn        TermSizeFunc
	frameBuffer   *Frame // Holds the reusable frame buffer
}

func NewEngine(cfg *Config, r *rand.Rand, factory DropFactory, sizeFn TermSizeFunc) *Engine {
	e := &Engine{
		height:      0,
		width:       0,
		baseColor:   cfg.BaseColor,
		density:     cfg.Density,
		randGen:     r,
		factory:     factory,
		sizeFn:      sizeFn,
		frameBuffer: nil, // Will be initialized on first resize
	}
	e.trailColors = e.calcTrailColors(6)
	return e
}

func (e *Engine) Resize(h, w int) {
	if h == e.height && w == e.width {
		return
	}
	e.height, e.width = h, w

	newDrops := make([][]Drop, w)
	for i := 0; i < w; i++ {
		n := int(e.density)
		if e.randGen.Float64() < e.density-float64(n) {
			n++
		}
		if n < 1 {
			n = 1
		}

		if i < len(e.drops) {
			newDrops[i] = e.drops[i]
			if len(newDrops[i]) > n {
				newDrops[i] = newDrops[i][:n]
			} else if len(newDrops[i]) < n {
				for j := len(newDrops[i]); j < n; j++ {
					newDrops[i] = append(newDrops[i], e.factory.CreateDrop(e.height))
				}
			}
		} else {
			for j := 0; j < n; j++ {
				newDrops[i] = append(newDrops[i], e.factory.CreateDrop(e.height))
			}
		}
	}
	e.drops = newDrops
	e.frameBuffer = NewFrame(h, w) // Reallocate the buffer when resizing
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

func (e *Engine) NextFrame() *Frame {
	if h, w, err := e.sizeFn(); err == nil && (h != e.height || w != e.width) {
		e.Resize(h, w)
	}

	e.frameBuffer.clear()
	for col, dd := range e.drops {
		for i := range dd {
			e.updateDrop(&dd[i])
			e.drawDrop(&dd[i], e.frameBuffer, col)
		}
	}
	return e.frameBuffer
}

func (e *Engine) updateDrop(d *Drop) {
	if !d.active {
		chance := 0.005 * e.density
		if e.density > 1.0 {
			chance = 0.005 + (e.density-1.0)*0.02
		}
		if e.randGen.Float64() < chance {
			d.active = true
			d.pos = 0
			d.length = e.randGen.Intn(12) + 8
			d.char = e.factory.CreateDrop(e.height).char
		}
		return
	}
	d.pos++
	if d.pos-d.length > e.height {
		d.pos = -d.length
		d.length = e.randGen.Intn(12) + 8
		d.char = e.factory.CreateDrop(e.height).char
		pause := 0.15 - e.density*0.05
		if e.density > 1.0 {
			pause = 0.05 - (e.density-1.0)*0.02
		}
		if pause < 0.01 {
			pause = 0.01
		}
		if e.randGen.Float64() < pause {
			d.active = false
		}
	}
}

func (e *Engine) drawDrop(d *Drop, f *Frame, col int) {
	if !d.active {
		return
	}
	tail := d.pos - d.length
	for row := tail; row <= d.pos; row++ {
		if row >= 0 && row < f.height {
			f.chars[row][col] = d.char
			f.isBg[row][col] = false
			dist := d.pos - row
			idx := int(float64(dist) / float64(d.length) * float64(len(e.trailColors)))
			if idx >= len(e.trailColors) {
				idx = len(e.trailColors) - 1
			}
			f.colors[row][col] = e.trailColors[idx]
		}
	}
}

// ---------- FRAME -----------------------------------------------------------

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

// ---------- RENDERER --------------------------------------------------------

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
	} else {
		s.deltaRender(f)
	}
	if s.previousFrame == nil || s.previousFrame.height != f.height || s.previousFrame.width != f.width {
		s.previousFrame = NewFrame(f.height, f.width)
	}
	s.copyFrame(f, s.previousFrame)
}

func (s *Screen) fullRender(f *Frame) {
	var b strings.Builder
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
	b.WriteString("\x1b[0m")
	s.out.Write([]byte(b.String()))
}

func (s *Screen) deltaRender(f *Frame) {
	var b strings.Builder
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
		b.WriteString("\x1b[0m")
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

// ---------- UTILS -----------------------------------------------------------

func min[T int | float64](a, b T) T {
	if a < b {
		return a
	}
	return b
}

// ---------- MAIN ------------------------------------------------------------

func main() {
	cfg, err := ParseFlags()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
		os.Exit(1)
	}
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	h, w, err := GetTermSize()
	if err != nil || h <= 0 || w <= 0 {
		fmt.Fprintln(os.Stderr, "Cannot get terminal size:", err)
		os.Exit(1)
	}
	SetupTerminal()
	defer RestoreTerminal()

	factory := NewRandomFactory(rng, h, cfg.CharSet)
	engine := NewEngine(cfg, rng, factory, GetTermSize)
	screen := NewScreen(os.Stdout)

	// Initial setup
	engine.Resize(h, w)

	frameDuration := time.Second / time.Duration(cfg.FPS)
	tick := time.NewTicker(frameDuration)
	defer tick.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
			screen.Draw(engine.NextFrame())
		}
	}
}
