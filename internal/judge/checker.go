package judge

import (
	"math"
	"strconv"
	"strings"
)

// CheckOutput compares output without executing an external checker.
// trim ignores trailing horizontal whitespace and trailing empty lines, while
// float compares whitespace-delimited numeric tokens by absolute OR relative error.
func CheckOutput(mode, expected, actual string, eps float64) bool {
	expected = normalizeNewlines(expected)
	actual = normalizeNewlines(actual)
	switch mode {
	case "exact":
		return expected == actual
	case "", "trim":
		return trimOutput(expected) == trimOutput(actual)
	case "tokens", "token":
		return strings.Join(strings.Fields(expected), "\x00") == strings.Join(strings.Fields(actual), "\x00")
	case "float":
		if eps <= 0 || math.IsNaN(eps) || math.IsInf(eps, 0) { eps = 1e-6 }
		want, got := strings.Fields(expected), strings.Fields(actual)
		if len(want) != len(got) { return false }
		for i, s := range want {
			a, ae := strconv.ParseFloat(s, 64)
			b, be := strconv.ParseFloat(got[i], 64)
			if ae != nil || be != nil { if s != got[i] { return false }; continue }
			if math.IsNaN(a) || math.IsNaN(b) || math.IsInf(a, 0) || math.IsInf(b, 0) { return false }
			diff := math.Abs(a-b)
			if diff > eps && diff/math.Max(1, math.Abs(a)) > eps { return false }
		}
		return true
	default:
		return false
	}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(strings.ReplaceAll(s, "\r\n", "\n"), "\r", "\n")
}

func trimOutput(s string) string {
	lines := strings.Split(s, "\n")
	for i := range lines { lines[i] = strings.TrimRight(lines[i], " \t\r") }
	return strings.TrimRight(strings.Join(lines, "\n"), "\n")
}
