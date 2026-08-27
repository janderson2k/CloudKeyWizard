package main

import (
	"fmt"
	"image"
	"image/color"
	"log"
	"os"
	"sync"
	"syscall"
	"unsafe"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// This replaces jnovack/cloudkey's own front-panel LCD app for the purpose of showing this
// device's hostname on the physical display. Deliberately NOT using CGO (this project cross-
// compiles with CGO_ENABLED=0 from a Windows dev machine -- introducing a cgo dependency here
// would mean setting up a full ARM64 cross C toolchain, a real reliability risk for the one
// binary this whole project depends on), so the kernel framebuffer ioctl structs below are
// hand-defined in pure Go rather than pulled from <linux/fb.h> via cgo. Struct layout mistakes
// here are a real risk with real consequences (a wrong smem_len could size the mmap wrong), so
// every value read from the kernel is sanity-checked before it's trusted for anything -- if
// anything looks implausible, this backs off and logs clearly rather than guessing forward.
//
// Design bet (see QUE.MD for the full reasoning): AppArmor confinement is normally bound to one
// exact executable path. jnovack's cloudkey binary at /usr/local/bin/cloudkey has been confirmed
// (from a real device's kernel log) to get EPERM opening /dev/fb0 despite running as root with no
// device-cgroup restrictions in its own systemd unit -- the signature of a profile scoped to that
// exact path. This binary, at a different path entirely, is expected to fall outside that
// profile's confinement and open the device freely. Not yet confirmed against real hardware.

const fbDevice = "/dev/fb0"

// --- hand-defined kernel ABI structs (linux/fb.h, 64-bit) ------------------

type fbBitfield struct {
	Offset   uint32
	Length   uint32
	MsbRight uint32
}

// fbVarScreeninfo mirrors struct fb_var_screeninfo. Every field here is a 4-byte-aligned uint32
// (including the embedded fbBitfield structs, themselves 3x uint32), so there's no padding
// ambiguity in this one -- it's the "safe" half of the two structs.
type fbVarScreeninfo struct {
	Xres, Yres, XresVirtual, YresVirtual   uint32
	Xoffset, Yoffset, BitsPerPixel, Grayscale uint32
	Red, Green, Blue, Transp               fbBitfield
	Nonstd, Activate, Height, Width        uint32
	AccelFlags, Pixclock                   uint32
	LeftMargin, RightMargin                uint32
	UpperMargin, LowerMargin               uint32
	HsyncLen, VsyncLen, Sync, Vmode        uint32
	Rotate, Colorspace                     uint32
	Reserved                               [4]uint32
}

// fbFixScreeninfo mirrors struct fb_fix_screeninfo on a 64-bit target. This one DOES have real
// padding: `unsigned long` fields are 8 bytes on arm64 and need 8-byte alignment, which the C
// compiler inserts padding for around the id[16]/u16 fields to satisfy -- explicit `_padN` fields
// below reproduce that exactly rather than relying on Go's own layout inference to happen to
// match. Total size 80 bytes, matching the well-known size of this struct on 64-bit Linux.
type fbFixScreeninfo struct {
	ID           [16]byte
	SmemStart    uint64
	SmemLen      uint32
	Type         uint32
	TypeAux      uint32
	Visual       uint32
	XPanStep     uint16
	YPanStep     uint16
	YWrapStep    uint16
	_pad1        uint16
	LineLength   uint32
	_pad2        uint32
	MmioStart    uint64
	MmioLen      uint32
	Accel        uint32
	Capabilities uint16
	Reserved1    uint16
	Reserved2    uint16
	_pad3        uint16
}

const (
	fbioGetVScreenInfo = 0x4600
	fbioGetFScreenInfo = 0x4602
)

func fbIoctl(fd uintptr, request uintptr, arg unsafe.Pointer) error {
	_, _, errno := syscall.Syscall(syscall.SYS_IOCTL, fd, request, uintptr(arg))
	if errno != 0 {
		return errno
	}
	return nil
}

type FrameBuffer struct {
	mu     sync.Mutex
	file   *os.File
	pixels []byte
	xres   int
	yres   int
	bpp    int
	stride int
	format string // "rgb565" or "xrgb8888" -- anything else is rejected at Open time
}

// OpenFrameBuffer opens and validates /dev/fb0. Returns a clear error (never panics) if the
// device is missing, the ioctls fail, the geometry looks implausible, or the pixel format isn't
// one of the two supported here -- callers must treat any error as "no framebuffer available
// this run" and continue without one, not as fatal.
func OpenFrameBuffer() (*FrameBuffer, error) {
	file, err := os.OpenFile(fbDevice, os.O_RDWR, 0)
	if err != nil {
		return nil, fmt.Errorf("opening %s: %w", fbDevice, err)
	}

	var varInfo fbVarScreeninfo
	if err := fbIoctl(file.Fd(), fbioGetVScreenInfo, unsafe.Pointer(&varInfo)); err != nil {
		file.Close()
		return nil, fmt.Errorf("FBIOGET_VSCREENINFO: %w", err)
	}
	var fixInfo fbFixScreeninfo
	if err := fbIoctl(file.Fd(), fbioGetFScreenInfo, unsafe.Pointer(&fixInfo)); err != nil {
		file.Close()
		return nil, fmt.Errorf("FBIOGET_FSCREENINFO: %w", err)
	}

	// Sanity checks -- a struct-layout mistake would show up here as an implausible value, not a
	// crash, which is the whole point of checking rather than trusting the raw ioctl result.
	if varInfo.Xres == 0 || varInfo.Xres > 8192 || varInfo.Yres == 0 || varInfo.Yres > 8192 {
		file.Close()
		return nil, fmt.Errorf("implausible resolution %dx%d -- refusing to trust this ioctl result (possible struct layout mismatch)", varInfo.Xres, varInfo.Yres)
	}
	if fixInfo.LineLength == 0 || fixInfo.LineLength > 65536 {
		file.Close()
		return nil, fmt.Errorf("implausible line_length %d -- refusing to trust this ioctl result", fixInfo.LineLength)
	}
	if fixInfo.SmemLen == 0 || uint64(fixInfo.SmemLen) > 256*1024*1024 {
		file.Close()
		return nil, fmt.Errorf("implausible smem_len %d -- refusing to trust this ioctl result", fixInfo.SmemLen)
	}

	var format string
	switch {
	case varInfo.BitsPerPixel == 16 && varInfo.Red.Length == 5 && varInfo.Green.Length == 6 && varInfo.Blue.Length == 5:
		format = "rgb565"
	case varInfo.BitsPerPixel == 32 && varInfo.Red.Length == 8 && varInfo.Green.Length == 8 && varInfo.Blue.Length == 8:
		format = "xrgb8888"
	default:
		file.Close()
		return nil, fmt.Errorf("unsupported pixel format: %d bpp, R%d/G%d/B%d bits -- only 16bpp RGB565 and 32bpp XRGB8888 are handled", varInfo.BitsPerPixel, varInfo.Red.Length, varInfo.Green.Length, varInfo.Blue.Length)
	}

	pixels, err := syscall.Mmap(int(file.Fd()), 0, int(fixInfo.SmemLen), syscall.PROT_READ|syscall.PROT_WRITE, syscall.MAP_SHARED)
	if err != nil {
		file.Close()
		return nil, fmt.Errorf("mmap: %w", err)
	}

	log.Printf("framebuffer opened: %dx%d, %s, line_length=%d, smem_len=%d", varInfo.Xres, varInfo.Yres, format, fixInfo.LineLength, fixInfo.SmemLen)

	return &FrameBuffer{
		file: file, pixels: pixels,
		xres: int(varInfo.Xres), yres: int(varInfo.Yres), bpp: int(varInfo.BitsPerPixel),
		stride: int(fixInfo.LineLength), format: format,
	}, nil
}

func (fb *FrameBuffer) Close() {
	fb.mu.Lock()
	defer fb.mu.Unlock()
	_ = syscall.Munmap(fb.pixels)
	_ = fb.file.Close()
}

// setPixel writes one pixel, bounds-checked against the actual mmap'd buffer length on every call
// -- deliberately not trusting xres/yres/stride alone to imply a safe offset, given none of those
// came from a source this code can independently verify.
func (fb *FrameBuffer) setPixel(x, y int, c color.RGBA) {
	if x < 0 || x >= fb.xres || y < 0 || y >= fb.yres {
		return
	}
	switch fb.format {
	case "rgb565":
		offset := y*fb.stride + x*2
		if offset+1 >= len(fb.pixels) {
			return
		}
		v := (uint16(c.R>>3) << 11) | (uint16(c.G>>2) << 5) | uint16(c.B>>3)
		fb.pixels[offset] = byte(v)
		fb.pixels[offset+1] = byte(v >> 8)
	case "xrgb8888":
		offset := y*fb.stride + x*4
		if offset+3 >= len(fb.pixels) {
			return
		}
		fb.pixels[offset] = c.B
		fb.pixels[offset+1] = c.G
		fb.pixels[offset+2] = c.R
		fb.pixels[offset+3] = 0
	}
}

func (fb *FrameBuffer) clear(c color.RGBA) {
	for y := 0; y < fb.yres; y++ {
		for x := 0; x < fb.xres; x++ {
			fb.setPixel(x, y, c)
		}
	}
}

// DrawLines clears the screen and draws each line of text top-to-bottom using a small embedded
// bitmap font (golang.org/x/image/font/basicfont -- a real, standard Go library, not a hand-rolled
// renderer) in white on black. Deliberately simple for a first version: no multi-screen cycling,
// no status icons -- just readable text, matching the intentionally small scope agreed for this
// rebuild (see QUE.MD).
func (fb *FrameBuffer) DrawLines(lines []string) {
	fb.mu.Lock()
	defer fb.mu.Unlock()

	fb.clear(color.RGBA{0, 0, 0, 255})

	face := basicfont.Face7x13
	lineHeight := face.Height + 2
	y := lineHeight
	for _, line := range lines {
		if y > fb.yres {
			break
		}
		drawer := &font.Drawer{
			Dst:  fbImageAdapter{fb},
			Src:  image.NewUniform(color.RGBA{255, 255, 255, 255}),
			Face: face,
			Dot:  fixed.P(2, y),
		}
		drawer.DrawString(line)
		y += lineHeight
	}
}

// fbImageAdapter lets font.Drawer (which wants a draw.Image) write straight into the framebuffer
// via setPixel, without an intermediate image.RGBA copy -- simpler and avoids a second full-frame
// buffer for a display this small.
type fbImageAdapter struct{ fb *FrameBuffer }

func (a fbImageAdapter) ColorModel() color.Model { return color.RGBAModel }
func (a fbImageAdapter) Bounds() image.Rectangle { return image.Rect(0, 0, a.fb.xres, a.fb.yres) }
func (a fbImageAdapter) At(x, y int) color.Color { return color.RGBA{0, 0, 0, 255} }
func (a fbImageAdapter) Set(x, y int, c color.Color) {
	r, g, b, alpha := c.RGBA()
	if alpha == 0 {
		return
	}
	a.fb.setPixel(x, y, color.RGBA{byte(r >> 8), byte(g >> 8), byte(b >> 8), byte(alpha >> 8)})
}
