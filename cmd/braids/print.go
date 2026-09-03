package main

import (
	"fmt"
	"io"
)

// printer accumulates the first write error so command bodies stay readable
// while still reporting a failed write once, at the end.
//
// Writes to stdout that hit a closed pipe are handled by the Go runtime, which
// raises SIGPIPE as Unix expects. This exists for every other case — a full
// disk, or output redirected to a file or buffer — where dropping the error
// would let braids report success for output it never wrote.
type printer struct {
	w   io.Writer
	err error
}

func newPrinter(w io.Writer) *printer { return &printer{w: w} }

// Write lets a printer back an io.Writer such as tabwriter.
func (p *printer) Write(b []byte) (int, error) {
	if p.err != nil {
		return 0, p.err
	}
	n, err := p.w.Write(b)
	if err != nil {
		p.err = err
	}
	return n, err
}

func (p *printer) printf(format string, a ...any) {
	if p.err == nil {
		_, p.err = fmt.Fprintf(p.w, format, a...)
	}
}

// Err reports the first write failure, if any.
func (p *printer) Err() error { return p.err }
