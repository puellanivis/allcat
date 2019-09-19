package main

import (
	"io"
)

func splitOnNonprint(data []byte) [][]byte {
	var fields [][]byte
	var last int
	for i := 0; i < len(data); i++ {
		if data[i] < 32 || data[i] >= 127 {
			fields = append(fields, data[last:i+1:i+1])
			last = i + 1
		}
	}
	if last != len(data) {
		fields = append(fields, data[last:])
	}
	return fields
}

type nonprintReplacer struct {
	io.WriteCloser
}

func (w *nonprintReplacer) Write(data []byte) (n int, err error) {
	fields := splitOnNonprint(data)

	ctrl := []byte("^@")
	meta := []byte("M-^@")

	for _, field := range fields {
		if len(field) < 1 {
			continue
		}

		c, short := field[len(field)-1], field[:len(field)-1]

		switch {
		case c < 32 && c != '\n' && c != '\t':
			written, err := w.WriteCloser.Write(short)
			n += written
			if err != nil {
				return n, err
			}
			ctrl[1] = c + '@'
			if _, err := w.WriteCloser.Write(ctrl); err != nil {
				return n, err
			}
			n++

		case c < 127:
			written, err := w.WriteCloser.Write(field)
			n += written
			if err != nil {
				return n, err
			}

		case c == 127:
			written, err := w.WriteCloser.Write(short)
			n += written
			if err != nil {
				return n, err
			}
			ctrl[1] = '?'
			if _, err := w.WriteCloser.Write(ctrl); err != nil {
				return n, err
			}
			n++

		case c < 128+32:
			written, err := w.WriteCloser.Write(short)
			n += written
			if err != nil {
				return n, err
			}
			meta[2], meta[3] = '^', c-128+'@'
			if _, err := w.WriteCloser.Write(meta); err != nil {
				return n, err
			}
			n++

		case c == 255:
			written, err := w.WriteCloser.Write(short)
			n += written
			if err != nil {
				return n, err
			}
			meta[2], meta[3] = '^', '?'
			if _, err := w.WriteCloser.Write(meta); err != nil {
				return n, err
			}
			n++

		default:
			written, err := w.WriteCloser.Write(short)
			n += written
			if err != nil {
				return n, err
			}
			meta[2] = c - 128
			if _, err := w.WriteCloser.Write(meta[:3]); err != nil {
				return n, err
			}
			n++
		}
	}

	return n, err
}

func splitOnByte(data []byte, sep byte) [][]byte {
	var fields [][]byte
	var last int
	for i := 0; i < len(data); i++ {
		if data[i] == sep {
			fields = append(fields, data[last:i+1:i+1])
			last = i + 1
		}
	}
	if last != len(data) {
		fields = append(fields, data[last:])
	}
	return fields
}

type byteReplacer struct {
	io.WriteCloser
	sep  byte
	with []byte
}

func (w *byteReplacer) Write(data []byte) (n int, err error) {
	fields := splitOnByte(data, w.sep)

	for _, field := range fields {
		if len(field) < 1 {
			continue
		}

		if field[len(field)-1] != w.sep {
			written, err := w.WriteCloser.Write(field)
			n += written
			if err != nil {
				return n, err
			}
			continue
		}

		written, err := w.WriteCloser.Write(field[:len(field)-1])
		n += written
		if err != nil {
			return n, err
		}
		if _, err := w.WriteCloser.Write(w.with); err != nil {
			return n, err
		}
		n++
	}

	return n, err
}
