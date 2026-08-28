package cli

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
)

func TestRenderTable_GIVEN_headersAndRows_WHEN_rendered_THEN_columnsWidthAndLayoutMatchSpec(t *testing.T) {
	var buf bytes.Buffer

	renderTable(&buf, false, []string{"Slot", "Name", "Branch", "Path"}, [][]string{
		{"-", "clone-tree (main)", "main", "/repo/clone-tree"},
		{"-", "feature", "feature-x", "/repo/clone-tree-worktrees/feature"},
	})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	// leading blank, header, underline, 2 data rows, trailing blank = 6 lines.
	if len(lines) != 6 {
		t.Fatalf("got %d lines, want 6: %#v", len(lines), lines)
	}
	if lines[0] != "" {
		t.Fatalf("expected leading blank line, got %q", lines[0])
	}
	if lines[5] != "" {
		t.Fatalf("expected trailing blank line, got %q", lines[5])
	}

	// Widths: Slot=max(4,1)=4, Name=max(4,17,7)=17 ("clone-tree (main)" is
	// 17 runes), Branch=max(6,4,9)=9, Path=max(4,17,34)=34
	// ("/repo/clone-tree-worktrees/feature" is 34 runes).
	wantHeader := fmt.Sprintf("  %-4s  %-17s  %-9s  %-34s", "Slot", "Name", "Branch", "Path")
	if lines[1] != wantHeader {
		t.Fatalf("got header %q, want %q", lines[1], wantHeader)
	}
	wantRow2 := fmt.Sprintf("  %-4s  %-17s  %-9s  %-34s", "-", "feature", "feature-x", "/repo/clone-tree-worktrees/feature")
	if lines[4] != wantRow2 {
		t.Fatalf("got row %q, want %q", lines[4], wantRow2)
	}
}

func TestRenderTable_GIVEN_rows_WHEN_rendered_THEN_underlineLengthEqualsColumnWidth(t *testing.T) {
	var buf bytes.Buffer

	renderTable(&buf, false, []string{"Slot", "Name"}, [][]string{
		{"-", "verylongworktreename"},
	})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	underline := lines[2]

	slotUnderline := strings.Repeat("─", len("Slot"))
	nameUnderline := strings.Repeat("─", len("verylongworktreename"))
	want := "  " + slotUnderline + "  " + nameUnderline
	if underline != want {
		t.Fatalf("got underline %q, want %q", underline, want)
	}
}

func TestRenderTable_GIVEN_nonTTY_WHEN_rendered_THEN_noAnsiCodesEmitted(t *testing.T) {
	var buf bytes.Buffer

	renderTable(&buf, false, []string{"Name"}, [][]string{{"feature"}})

	if strings.Contains(buf.String(), ansiBold) || strings.Contains(buf.String(), ansiReset) {
		t.Fatalf("expected no ANSI codes in non-TTY output, got %q", buf.String())
	}
}

func TestRenderTable_GIVEN_ttyBold_WHEN_rendered_THEN_onlyHeaderRowWrappedInAnsi(t *testing.T) {
	var buf bytes.Buffer

	renderTable(&buf, true, []string{"Name"}, [][]string{{"feature"}})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	header := lines[1]
	underline := lines[2]
	dataRow := lines[3]

	// header width = max(len("Name"), len("feature")) = 7.
	wantHeader := ansiBold + "  Name   " + ansiReset
	if header != wantHeader {
		t.Fatalf("got header %q, want %q", header, wantHeader)
	}
	if strings.Contains(underline, ansiBold) || strings.Contains(dataRow, ansiBold) {
		t.Fatalf("expected only header row to carry ANSI bold, got underline %q data %q", underline, dataRow)
	}
}

func TestRenderTable_GIVEN_unicodeValue_WHEN_widthComputed_THEN_countedInRunesNotBytes(t *testing.T) {
	var buf bytes.Buffer

	// "wörktree" is 8 runes but 9 bytes (ö is a 2-byte UTF-8 sequence).
	renderTable(&buf, false, []string{"Name"}, [][]string{{"wörktree"}})

	lines := strings.Split(strings.TrimSuffix(buf.String(), "\n"), "\n")
	underline := lines[2]
	want := "  " + strings.Repeat("─", len([]rune("wörktree")))
	if underline != want {
		t.Fatalf("got underline %q, want %q (byte-width would be wrong)", underline, want)
	}
}
