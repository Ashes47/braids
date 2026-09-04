package tui

import "github.com/Ashes47/braids/internal/brand"

// The header draws the largest mark that still leaves room for the facts and
// every key binding, and none at all when neither fits.
func logoSizes() [][]string { return [][]string{brand.Full(), brand.Small()} }
