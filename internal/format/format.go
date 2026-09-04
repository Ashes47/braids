// Package format holds the few pieces of presentation the terminal UI and the
// command line have to agree on. A size printed one way in a table and another
// way on a screen reads as two different numbers.
package format

import "fmt"

// Bytes renders a size compactly, in the binary units a filesystem reports.
func Bytes(n int64) string {
	switch {
	case n >= 1<<30:
		return fmt.Sprintf("%.1f GB", float64(n)/(1<<30))
	case n >= 1<<20:
		return fmt.Sprintf("%.0f MB", float64(n)/(1<<20))
	case n >= 1<<10:
		return fmt.Sprintf("%.0f kB", float64(n)/(1<<10))
	default:
		return fmt.Sprintf("%d B", n)
	}
}
