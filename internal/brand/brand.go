// Package brand holds braids' mark. It sits on its own so the terminal UI and
// the command line can both draw it without one importing the other.
package brand

// Full is braids set in the angular ASCII face. Pure ASCII, so it needs no
// --ascii twin: the glyphs are the same either way. Every line is padded to one
// width, so the block can be aligned as a unit without measuring each row.
func Full() []string {
	return []string{
		"  ___.                          __        .___         ",
		"  \\_ |__   ______     _____    |__|    __| _/    ______",
		"   | __ \\  \\_  __ \\   \\__  \\    |     / __ |    /  ___/",
		"   | \\_\\ \\  |  | \\/    / __ \\_  |    / /_/ |    \\___  \\",
		"   |___  /  |__|      (____  / |__|  \\____ |   /____  /",
		"       \\/                  \\/             \\/        \\/ ",
	}
}

// Small is the same word in a narrower face, for a header that also has to
// hold the facts and the legend. Setting the mark smaller beats losing it on
// any terminal narrower than very wide.
func Small() []string {
	return []string{
		" _                    _      _      ",
		"| |__   _ __    __ _ (_)  __| | ___ ",
		"| '_ \\ | '__|  / _` || | / _` |/ __|",
		"| |_) || |    | (_| || || (_| |\\__ \\",
		"|_.__/ |_|     \\__,_||_| \\__,_||___/",
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
