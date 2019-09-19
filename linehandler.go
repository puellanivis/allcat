package main

import (
	"fmt"
	"io"
)

func splitLines(data []byte) [][]byte {
	var fields [][]byte
	var last int
	for i := 0; i < len(data); i++ {
		if data[i] == '\n' {
			fields = append(fields, data[last:i+1:i+1])
			last = i + 1
		}
	}
	if last != len(data) {
		fields = append(fields, data[last:])
	}
	return fields
}

type lineNumberer struct {
	io.WriteCloser
	lineno   int
	suppress bool
}

func (w *lineNumberer) Write(data []byte) (n int, err error) {
	lines := splitLines(data)

	for _, line := range lines {
		if !w.suppress {
			w.lineno++
			if _, err := fmt.Fprintf(w.WriteCloser, "%6d\t", w.lineno); err != nil {
				return n, err
			}
		}

		written, err := w.WriteCloser.Write(line)
		n += written
		if err != nil {
			return n, err
		}

		w.suppress = len(line) < 1 || line[len(line)-1] != '\n'
	}

	return n, nil
}

type nonblankLineNumberer struct {
	io.WriteCloser
	lineno   int
	suppress bool
}

func (w *nonblankLineNumberer) Write(data []byte) (n int, err error) {
	lines := splitLines(data)

	for _, line := range lines {
		if len(line) < 1 || line[0] == '\n' {
			w.suppress = true
		}

		if !w.suppress {
			w.lineno++
			if _, err := fmt.Fprintf(w.WriteCloser, "%6d\t", w.lineno); err != nil {
				return n, err
			}
		}

		written, err := w.WriteCloser.Write(line)
		n += written
		if err != nil {
			return n, err
		}

		w.suppress = len(line) < 1 || line[len(line)-1] != '\n'
	}

	return n, nil
}

type blankSqueezer struct {
	io.WriteCloser
	lastWasBlank bool
}

func (w *blankSqueezer) Write(data []byte) (n int, err error) {
	lines := splitLines(data)

	for _, line := range lines {
		if len(line) < 1 {
			continue
		}

		if line[len(line)-1] != '\n' {
			written, err := w.WriteCloser.Write(line)
			n += written
			if err != nil {
				return n, err
			}
			continue
		}

		if line[0] == '\n' {
			if w.lastWasBlank {
				n++ // we “wrote” this value from the input.
				continue
			}

			written, err := w.WriteCloser.Write(line)
			n += written
			if err != nil {
				return n, err
			}
			w.lastWasBlank = true
			continue
		}
		w.lastWasBlank = false

		written, err := w.WriteCloser.Write(line)
		n += written
		if err != nil {
			return n, err
		}
	}

	return n, nil
}
