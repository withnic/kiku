package cli

import (
	"bytes"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/withnic/kiku/internal/engine"
)

// writeDiff emits one hunk spanning the changes with up to three context
// lines on either side. Lines retain their original terminators.
func writeDiff(out io.Writer, file engine.FileOutput) error {
	if bytes.Equal(file.Original, file.Fixed) {
		return nil
	}
	oldLines := diffLines(file.Original)
	newLines := diffLines(file.Fixed)
	first := 0
	for first < min(len(oldLines), len(newLines)) && oldLines[first] == newLines[first] {
		first++
	}
	oldEnd, newEnd := len(oldLines), len(newLines)
	for oldEnd > first && newEnd > first && oldLines[oldEnd-1] == newLines[newEnd-1] {
		oldEnd--
		newEnd--
	}
	start := max(0, first-3)
	context := min(3, len(oldLines)-oldEnd)
	var result strings.Builder
	fmt.Fprintf(&result, "--- %s\n+++ %s\n@@ -%s +%s @@\n", diffPath("a/"+file.Path), diffPath("b/"+file.Path),
		hunkRange(start, oldEnd+context), hunkRange(start, newEnd+context))
	appendDiffLines(&result, ' ', oldLines[start:first])
	appendDiffLines(&result, '-', oldLines[first:oldEnd])
	appendDiffLines(&result, '+', newLines[first:newEnd])
	appendDiffLines(&result, ' ', oldLines[oldEnd:oldEnd+context])
	_, err := io.WriteString(out, result.String())
	return err
}

func diffLines(data []byte) []string {
	lines := strings.SplitAfter(string(data), "\n")
	if lines[len(lines)-1] == "" {
		return lines[:len(lines)-1]
	}
	return lines
}

func appendDiffLines(out *strings.Builder, marker byte, lines []string) {
	for _, line := range lines {
		out.WriteByte(marker)
		out.WriteString(line)
		if !strings.HasSuffix(line, "\n") {
			out.WriteString("\n\\ No newline at end of file\n")
		}
	}
}

func hunkRange(start, end int) string {
	switch end - start {
	case 0:
		return fmt.Sprintf("%d,0", start)
	case 1:
		return strconv.Itoa(start + 1)
	default:
		return fmt.Sprintf("%d,%d", start+1, end-start)
	}
}

func diffPath(path string) string {
	if strings.ContainsAny(path, "\t\r\n\"\\") {
		return strconv.Quote(path)
	}
	return path
}
