package patcher

import (
	"strings"
)

// LevenshteinDistance calcula la distancia de edicion entre dos cadenas.
func LevenshteinDistance(s1, s2 string) int {
	r1, r2 := []rune(s1), []rune(s2)
	l1, l2 := len(r1), len(r2)

	if l1 == 0 {
		return l2
	}
	if l2 == 0 {
		return l1
	}

	dp := make([][]int, l1+1)
	for i := range dp {
		dp[i] = make([]int, l2+1)
		dp[i][0] = i
	}
	for j := 0; j <= l2; j++ {
		dp[0][j] = j
	}

	for i := 1; i <= l1; i++ {
		for j := 1; j <= l2; j++ {
			cost := 0
			if r1[i-1] != r2[j-1] {
				cost = 1
			}
			dp[i][j] = min(
				dp[i-1][j]+1,      // eliminacion
				dp[i][j-1]+1,      // insercion
				dp[i-1][j-1]+cost, // sustitucion
			)
		}
	}

	return dp[l1][l2]
}

// Similarity retorna un puntaje entre 0.0 y 1.0 representando la similitud entre dos cadenas.
func Similarity(s1, s2 string) float64 {
	maxLen := max(len(s1), len(s2))
	if maxLen == 0 {
		return 1.0
	}
	dist := LevenshteinDistance(s1, s2)
	return 1.0 - (float64(dist) / float64(maxLen))
}

// FindFuzzyMatch busca el segmento de lineas en content que mejor coincide con search.
// Retorna (startLineIndex, endLineIndexExcl, similarity, found).
func FindFuzzyMatch(content, search string, threshold float64) (int, int, float64, bool) {
	contentLines := splitLines(content)
	searchLines := splitLines(search)

	nSearch := len(searchLines)
	nContent := len(contentLines)

	if nSearch == 0 || nContent == 0 || nSearch > nContent {
		return -1, -1, 0, false
	}

	normalizedSearch := normalizeLines(searchLines)
	searchJoined := strings.Join(normalizedSearch, "\n")

	bestScore := 0.0
	bestStart := -1
	bestEnd := -1

	// Ventana deslizante de tamano nSearch (con margen de +- 1 linea si nSearch > 2)
	windowSizes := []int{nSearch}
	if nSearch > 2 {
		windowSizes = append(windowSizes, nSearch-1, nSearch+1)
	}

	for _, wSize := range windowSizes {
		if wSize > nContent || wSize <= 0 {
			continue
		}

		for i := 0; i <= nContent-wSize; i++ {
			windowSlice := contentLines[i : i+wSize]
			normalizedWindow := normalizeLines(windowSlice)
			windowJoined := strings.Join(normalizedWindow, "\n")

			// Si coinciden exactamente despues de normalizar espacios
			if windowJoined == searchJoined {
				return i, i + wSize, 1.0, true
			}

			score := Similarity(windowJoined, searchJoined)
			if score > bestScore {
				bestScore = score
				bestStart = i
				bestEnd = i + wSize
			}
		}
	}

	if bestScore >= threshold && bestStart >= 0 {
		return bestStart, bestEnd, bestScore, true
	}

	return -1, -1, bestScore, false
}

func splitLines(s string) []string {
	normalized := strings.ReplaceAll(s, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")
	return strings.Split(normalized, "\n")
}

func normalizeLines(lines []string) []string {
	out := make([]string, len(lines))
	for i, l := range lines {
		out[i] = strings.TrimSpace(l)
	}
	return out
}

func min(a, b, c int) int {
	m := a
	if b < m {
		m = b
	}
	if c < m {
		m = c
	}
	return m
}

func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
