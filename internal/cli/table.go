package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
)

const (
	ansiBold  = "\033[1m"
	ansiReset = "\033[0m"
)

// renderTable writes headers and rows as a table to w, mimicking
// tagpay-worktree.sh's cmd_list rendering: a leading blank line, a 2-space
// left margin, 2-space gutters between left-aligned columns sized to
// max(header, longest value), a bold header row (when bold is true), a
// per-column `─` underline row sized to the exact column width, the data
// rows, and a trailing blank line. Reused by future ports/hosts commands.
func renderTable(w io.Writer, bold bool, headers []string, rows [][]string) {
	widths := columnWidths(headers, rows)

	_, _ = fmt.Fprintln(w)
	writeTableRow(w, headers, widths, bold)
	writeTableRow(w, underlineRow(widths), widths, false)
	for _, row := range rows {
		writeTableRow(w, row, widths, false)
	}
	_, _ = fmt.Fprintln(w)
}

// columnWidths computes each column's width as max(header length, longest
// row value length), counted in runes so unicode names size correctly.
func columnWidths(headers []string, rows [][]string) []int {
	widths := make([]int, len(headers))
	for i, h := range headers {
		widths[i] = len([]rune(h))
	}
	for _, row := range rows {
		for i, cell := range row {
			if n := len([]rune(cell)); n > widths[i] {
				widths[i] = n
			}
		}
	}
	return widths
}

// underlineRow builds a `─` row where each column is repeated to its exact
// width.
func underlineRow(widths []int) []string {
	row := make([]string, len(widths))
	for i, wd := range widths {
		row[i] = strings.Repeat("─", wd)
	}
	return row
}

func writeTableRow(w io.Writer, cells []string, widths []int, bold bool) {
	padded := make([]string, len(cells))
	for i, cell := range cells {
		padded[i] = cell + strings.Repeat(" ", widths[i]-len([]rune(cell)))
	}
	line := "  " + strings.Join(padded, "  ")
	if bold {
		line = ansiBold + line + ansiReset
	}
	_, _ = fmt.Fprintln(w, line)
}

// stdoutIsTTY reports whether os.Stdout is attached to a terminal.
func stdoutIsTTY() bool {
	info, err := os.Stdout.Stat()
	if err != nil {
		return false
	}
	return info.Mode()&os.ModeCharDevice != 0
}
