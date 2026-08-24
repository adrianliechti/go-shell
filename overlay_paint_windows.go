//go:build windows

package shell

import (
	"unsafe"

	"golang.org/x/sys/windows"
)

// The caption buttons are painted rather than themed: DrawThemeBackground has no
// part list for the Windows 11 caption buttons (the shell paints them itself),
// so matching them means drawing the same thing — a flat fill in the title-bar
// colour, the standard hover/pressed wash, and the glyph from Segoe Fluent Icons
// (Segoe MDL2 Assets on Windows 10), which is where the shell takes them from.

// Segoe Fluent Icons / MDL2 code points for the caption glyphs.
const (
	glyphMinimize = "\uE921" // ChromeMinimize
	glyphMaximize = "\uE922" // ChromeMaximize
	glyphRestore  = "\uE923" // ChromeRestore
	glyphClose    = "\uE8BB" // ChromeClose
)

// Colours as 0x00BBGGRR COLORREFs, matching the system title bar. Close is the
// one button with its own hover colour, as everywhere else in Windows.
const (
	colorLightBackground = 0x00F8F8F8
	colorLightForeground = 0x00454545
	colorLightHover      = 0x00ECECEC
	colorLightPressed    = 0x00DDDDDD

	colorDarkBackground = 0x000E0E0E
	colorDarkForeground = 0x00C8C8C8
	colorDarkHover      = 0x00242424
	colorDarkPressed    = 0x00323232

	colorCloseHover   = 0x002319C7 // #C71923
	colorClosePressed = 0x004D42E8 // #E8424D
	colorCloseGlyph   = 0x00FFFFFF
)

func (o *overlay) colors() (background, foreground, hover, pressed uintptr) {
	if o.dark {
		return colorDarkBackground, colorDarkForeground, colorDarkHover, colorDarkPressed
	}

	return colorLightBackground, colorLightForeground, colorLightHover, colorLightPressed
}

func (o *overlay) paint() {
	var ps paintStruct

	hdc, _, _ := procBeginPaint.Call(o.hwnd, uintptr(unsafe.Pointer(&ps)))

	if hdc == 0 {
		return
	}

	defer procEndPaint.Call(o.hwnd, uintptr(unsafe.Pointer(&ps)))

	background, foreground, hover, pressed := o.colors()

	client := clientRect(o.hwnd)
	fill(hdc, client, background)

	// Slightly quieter than the full native caption, so the controls recede into
	// an application toolbar instead of reading as a separate title-bar block.
	font := createGlyphFont(9 * o.dpi / 72)

	if font == 0 {
		return
	}

	defer procDeleteObject.Call(font)

	previous, _, _ := procSelectObject.Call(hdc, font)
	defer procSelectObject.Call(hdc, previous)

	procSetBkMode.Call(hdc, transparentBkMode)

	for _, b := range o.buttons {
		r, ok := o.rectOf(b)

		if !ok {
			continue
		}

		glyphColor := foreground

		switch {
		case o.pressed == b:
			if b == buttonClose {
				fill(hdc, r, colorClosePressed)
				glyphColor = colorCloseGlyph
			} else {
				fill(hdc, r, pressed)
			}

		// A press in progress elsewhere must not light this button up.
		case o.hovered == b && o.pressed == buttonNone:
			if b == buttonClose {
				fill(hdc, r, colorCloseHover)
				glyphColor = colorCloseGlyph
			} else {
				fill(hdc, r, hover)
			}
		}

		procSetTextColor.Call(hdc, glyphColor)
		drawGlyph(hdc, r, o.glyph(b))
	}
}

func (o *overlay) glyph(b button) string {
	switch b {
	case buttonMinimize:
		return glyphMinimize

	case buttonMaximize:
		if isZoomed(o.parent) {
			return glyphRestore
		}

		return glyphMaximize

	case buttonClose:
		return glyphClose
	}

	return ""
}
func fill(hdc uintptr, r rect, color uintptr) {
	brush, _, _ := procCreateSolidBrush.Call(color)

	if brush == 0 {
		return
	}

	defer procDeleteObject.Call(brush)

	procFillRect.Call(hdc, uintptr(unsafe.Pointer(&r)), brush)
}

func drawGlyph(hdc uintptr, r rect, glyph string) {
	text := utf16(glyph)

	if text == nil {
		return
	}

	procDrawText.Call(
		hdc,
		uintptr(unsafe.Pointer(text)),
		^uintptr(0), // -1: NUL-terminated
		uintptr(unsafe.Pointer(&r)),
		dtCenter|dtVCenter|dtSingleLine|dtNoPrefix,
	)
}

// createGlyphFont picks the icon font the shell itself draws the caption with:
// Segoe Fluent Icons on Windows 11, Segoe MDL2 Assets before that. The choice is
// made from the OS build because CreateFontW silently substitutes another face
// for a missing one instead of failing, so it cannot be probed by trying it.
func createGlyphFont(height int32) uintptr {
	name := "Segoe MDL2 Assets"

	if _, _, build := windows.RtlGetNtVersionNumbers(); build >= 22000 {
		name = "Segoe Fluent Icons"
	}

	face := utf16(name)

	if face == nil {
		return 0
	}

	font, _, _ := procCreateFont.Call(
		uintptr(height),
		0, 0, 0,
		fwNormal,
		0, 0, 0,
		defaultCharset,
		0, 0, 0, 0,
		uintptr(unsafe.Pointer(face)),
	)

	return font
}
