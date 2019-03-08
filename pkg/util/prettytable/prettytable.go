// prettytable implements a unicode table formatter.
package prettytable

import (
	"fmt"
	"io"
	"strings"
)

// Row is a list of column elements.
type Row []string

// Table represents a table of data.  All headers and data rows
// must contain the same number of elements.
type Table struct {
	// Header is an list of column headings.
	Header Row
	// Rows is a list of rows of data.
	Rows []Row
	// Style allows you to apply custom styling.
	Style *Style
	// widths is used to record column widths.
	widths []int
}

// Style allows custom styling of a table
type Style struct {
	HeaderHorizontal rune
	HeaderVertical   rune
	Horizontal       rune
	Vertical         rune
	TopLeft          rune
	TopSeparator     rune
	TopRight         rune
	MiddleLeft       rune
	MiddleSeparator  rune
	MiddleRight      rune
	BottomLeft       rune
	BottomSeparator  rune
	BottomRight      rune
}

var (
	// StyleDefault is thin lines with square corners.
	StyleDefault = &Style{'─', '│', '─', '│', '┌', '┬', '┐', '├', '┼', '┤', '└', '┴', '┘'}
	// StyleBold is thick lines with square corners.
	StyleBold = &Style{'━', '┃', '━', '┃', '┏', '┳', '┓', '┣', '╋', '┫', '┗', '┻', '┛'}
	// StyleBoldHeader is thick lines on the header with square corners.
	StyleBoldHeader = &Style{'━', '┃', '─', '│', '┏', '┳', '┓', '┡', '╇', '┩', '└', '┴', '┘'}
	// StyleCurved is thin lines with curved corners.
	StyleCurved = &Style{'─', '│', '─', '│', '╭', '┬', '╮', '├', '┼', '┤', '╰', '┴', '╯'}
)

// GeometryError is returned when the row and headers lengths do not match.
type GeometryError struct{}

// NewGeometryError creates a new geometry error.
func NewGeometryError() error {
	return &GeometryError{}
}

// Error returns the error string for a GeometryError.
func (e GeometryError) Error() string {
	return "rows (and headers) have mismatched column lengths"
}

// getStyle returns the table style or a default.
func (t *Table) getStyle() *Style {
	if t.Style != nil {
		return t.Style
	}
	return StyleDefault
}

// updateMaxWidths takes a list of column widths and a row, updating if
// the current element length is greater than the existing one.
func (t *Table) updateMaxWidths(row Row) {
	for index, elem := range row {
		length := len(elem)
		if length > t.widths[index] {
			t.widths[index] = length
		}
	}
}

// formatFrame returns a string of a frame decoration, the table top and bottom
// and the header separator.  We specify the left, separator and right decorative
// characters.
func (t *Table) formatFrame(horizontal, left, separator, right rune) string {
	// Start with the left delimiter
	line := string(left)
	// For each column draw a horizontal line, adding a trailing
	// separator or delimiter.  Pad each columm with 2 extra characters.
	for index, width := range t.widths {
		line += strings.Repeat(string(horizontal), width+2)
		delimiter := string(separator)
		if index+1 == len(t.widths) {
			delimiter = string(right)
		}
		line += delimiter
	}
	return line
}

// formatRow returns the string of a header or row.
func (t *Table) formatRow(vertical rune, row Row) string {
	// Start with the left delimiter
	line := string(vertical)
	// For each column render the left justified element, with padding,
	// then add a trailing separator.
	for index, element := range row {
		format := fmt.Sprintf(" %%-%ds %%s", t.widths[index])
		line += fmt.Sprintf(format, element, string(vertical))
	}
	return line
}

// format formats the whole table as a string array.
func (t *Table) format() []string {
	style := t.getStyle()
	lines := []string{}
	lines = append(lines, t.formatFrame(style.HeaderHorizontal, style.TopLeft, style.TopSeparator, style.TopRight))
	lines = append(lines, t.formatRow(style.HeaderVertical, t.Header))
	lines = append(lines, t.formatFrame(style.HeaderHorizontal, style.MiddleLeft, style.MiddleSeparator, style.MiddleRight))
	for _, row := range t.Rows {
		lines = append(lines, t.formatRow(style.Vertical, row))
	}
	lines = append(lines, t.formatFrame(style.Horizontal, style.BottomLeft, style.BottomSeparator, style.BottomRight))
	return lines
}

// Write formats the table data and writes it a line at a time to the supplied writer.
func (t *Table) Write(w io.Writer) error {
	// Calculate the number of columns raising an error if any rows
	// are not the same size as the header.
	columns := len(t.Header)
	for _, row := range t.Rows {
		if len(row) != columns {
			return NewGeometryError()
		}
	}

	// Calculate the character width of each column based on the largest string
	// in each.
	t.widths = make([]int, columns)
	t.updateMaxWidths(t.Header)
	for _, row := range t.Rows {
		t.updateMaxWidths(row)
	}

	// Write out the header
	for _, line := range t.format() {
		if _, err := w.Write([]byte(line + "\n")); err != nil {
			return err
		}
	}
	return nil
}
