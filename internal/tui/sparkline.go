package tui

import (
	"fmt"
	"math"
	"strings"
)

var sparkBlocks = []rune(" ▁▂▃▄▅▆▇█")

// Sparkline renders values as a block-character sparkline of width w,
// right-aligned (latest value at the right edge).
func Sparkline(values []float64, w int) string {
	if w <= 0 {
		return ""
	}
	if len(values) > w {
		values = values[len(values)-w:]
	}
	max := 0.0
	for _, v := range values {
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for i := 0; i < w-len(values); i++ {
		b.WriteRune(' ')
	}
	for _, v := range values {
		idx := 0
		if max > 0 {
			idx = int(math.Round(v / max * float64(len(sparkBlocks)-1)))
		}
		if idx < 0 {
			idx = 0
		}
		if idx >= len(sparkBlocks) {
			idx = len(sparkBlocks) - 1
		}
		b.WriteRune(sparkBlocks[idx])
	}
	return b.String()
}

// HumanBytes renders a byte count as a human-readable string.
func HumanBytes(n int64) string {
	return humanFloat(float64(n), "B")
}

// HumanRate renders bytes/second.
func HumanRate(bps float64) string {
	return humanFloat(bps, "B/s")
}

func humanFloat(v float64, unit string) string {
	units := []string{"", "K", "M", "G", "T"}
	i := 0
	for v >= 1024 && i < len(units)-1 {
		v /= 1024
		i++
	}
	if i == 0 {
		return fmt.Sprintf("%.0f %s%s", v, units[i], unit)
	}
	return fmt.Sprintf("%.1f %s%s", v, units[i], unit)
}
