// Package brand holds braids' mark. It sits on its own so the terminal UI and
// the command line can both draw it without one importing the other.
package brand

// Full is braids set in the angular ASCII face. Pure ASCII, so it needs no
// --ascii twin: the glyphs are the same either way. Every line is padded to one
// width, so the block can be aligned as a unit without measuring each row.
//
// The letters are spaced one column apart, which the face does not do on its
// own: r ends two columns short of its cell, so r and a sat three apart while
// every other pair sat one, and i and d sat two. Measured by looking for
// columns with no ink in any row rather than by eye.
//
// That measure is not the whole story: it asks whether any row brings two
// letters close, and d and s never do. Measured per row instead, they sat an
// average of 4.6 columns apart against 2.2 for b and r, which is the gap the
// eye actually reads. s moves one column left, which is as close as it can
// come without touching the top stroke of d.
func Full() []string {
	return []string{
		"  ___.                        __       .___        ",
		"  \\_ |__   ______   _____    |__|   __| _/   ______",
		"   | __ \\  \\_  __ \\ \\__  \\    |    / __ |   /  ___/",
		"   | \\_\\ \\  |  | \\/  / __ \\_  |   / /_/ |   \\___  \\",
		"   |___  /  |__|    (____  / |__| \\____ |  /____  /",
		"       \\/                \\/            \\/       \\/ ",
	}
}

// Small is the same word in a narrower face, for a header that also has to
// hold the facts and the legend. Setting the mark smaller beats losing it on
// any terminal narrower than very wide.
//
// Its letters interlock, so there is far less to respace here than in Full: one
// column is blank in every row, between r and a, and it is taken. That leaves
// them an average of 2.75 columns apart rather than 3.75, still wider than the
// rest of the word, which is the face's own doing and not something that can be
// closed without the letters touching.
func Small() []string {
	return []string{
		" _                   _      _      ",
		"| |__   _ __   __ _ (_)  __| | ___ ",
		"| '_ \\ | '__| / _` || | / _` |/ __|",
		"| |_) || |   | (_| || || (_| |\\__ \\",
		"|_.__/ |_|    \\__,_||_| \\__,_||___/",
	}
}

// Width reports the column width of a block returned by Full or Small.
func Width(art []string) int {
	if len(art) == 0 {
		return 0
	}
	return len(art[0])
}

// Tagline is the one line that goes under the mark.
const Tagline = "conversations as a graph"
