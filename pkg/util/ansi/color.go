// package ansi provides ANSI escape sequence handing.
package ansi

import (
	"fmt"
)

const (
	// Escape is the ANSI escape character.
	Escape = '\x1B'
)

// ColorType defines the type of color.
type ColorType int

const (
	Black   ColorType = 30
	Red     ColorType = 31
	Green   ColorType = 32
	Yellow  ColorType = 33
	Blue    ColorType = 34
	Magenta ColorType = 35
	Cyan    ColorType = 36
	White   ColorType = 37
)

// Color returns an ANSI escape sequence that turns the output to the requested color.
func Color(c ColorType) string {
	return fmt.Sprintf("%c[1;%dm", Escape, int(c))
}

// Reset returns an ANSI escape sequence that resets all formatting.
func Reset() string {
	return "\x1b[0m"
}
