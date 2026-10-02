// Package textutil holds small string algorithms shared across packages.
package textutil

import (
	"strconv"
	"strings"
)

// Levenshtein returns the edit distance between a and b.
func Levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	cur := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev, cur = cur, prev
	}
	return prev[len(rb)]
}

// Closest returns the candidate within maxDistance edits of s (preferring
// the smallest distance, then lexical order), or "" if none is close.
func Closest(s string, candidates []string, maxDistance int) string {
	best, bestD := "", maxDistance+1
	for _, c := range candidates {
		if d := Levenshtein(s, c); d < bestD || (d == bestD && c < best) {
			best, bestD = c, d
		}
	}
	return best
}

// Thousands formats an integer with comma separators: 184,291.
func Thousands(n int64) string {
	neg := n < 0
	if neg {
		n = -n
	}
	s := strconv.FormatInt(n, 10)
	var b strings.Builder
	if neg {
		b.WriteByte('-')
	}
	for i, r := range s {
		if i > 0 && (len(s)-i)%3 == 0 {
			b.WriteByte(',')
		}
		b.WriteRune(r)
	}
	return b.String()
}
