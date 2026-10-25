package main

import (
	"io"
	"strings"

	"rsc.io/qr"
)

// printQR draws text as a QR code with half-block characters, two module
// rows per line. forDarkTerminal draws the light modules, which is what
// looks right on a dark background; otherwise the dark modules are drawn.
func printQR(w io.Writer, text string, forDarkTerminal bool) error {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return err
	}
	const quiet = 2
	n := code.Size
	dark := func(x, y int) bool {
		if x < 0 || y < 0 || x >= n || y >= n {
			return false
		}
		return code.Black(x, y)
	}
	ink := func(x, y int) bool { return dark(x, y) != forDarkTerminal }
	var b strings.Builder
	for y := -quiet; y < n+quiet; y += 2 {
		for x := -quiet; x < n+quiet; x++ {
			top, bottom := ink(x, y), ink(x, y+1)
			if y+1 >= n+quiet {
				bottom = false
			}
			switch {
			case top && bottom:
				b.WriteRune('█')
			case top:
				b.WriteRune('▀')
			case bottom:
				b.WriteRune('▄')
			default:
				b.WriteRune(' ')
			}
		}
		b.WriteByte('\n')
	}
	_, err = io.WriteString(w, b.String())
	return err
}

// qrPNG returns text as a PNG image.
func qrPNG(text string) ([]byte, error) {
	code, err := qr.Encode(text, qr.M)
	if err != nil {
		return nil, err
	}
	code.Scale = 8
	return code.PNG(), nil
}
