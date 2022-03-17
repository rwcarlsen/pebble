// Copyright (c) 2021 Canonical Ltd
//
// This program is free software: you can redistribute it and/or modify
// it under the terms of the GNU General Public License version 3 as
// published by the Free Software Foundation.
//
// This program is distributed in the hope that it will be useful,
// but WITHOUT ANY WARRANTY; without even the implied warranty of
// MERCHANTABILITY or FITNESS FOR A PARTICULAR PURPOSE.  See the
// GNU General Public License for more details.
//
// You should have received a copy of the GNU General Public License
// along with this program.  If not, see <http://www.gnu.org/licenses/>

package servicelog

import (
	"bytes"
	"io"
	"regexp"
	"sync"
	"time"
)

type formatter struct {
	mut             sync.Mutex
	serviceName     string
	dest            io.Writer
	writeTimestamp  bool
	timestampBuffer []byte
	timestamp       []byte
}

const (
	// outputTimeFormat is RFC3339 with millisecond precision.
	outputTimeFormat = "2006-01-02T15:04:05.000Z07:00"
)

// NewFormatWriter returns a io.Writer that inserts timestamp and service name for every
// line in the stream.
// For the input:
//   first\n
//   second\n
//   third\n
// The expected output is:
//   2021-05-13T03:16:51.001Z [test] first\n
//   2021-05-13T03:16:52.002Z [test] second\n
//   2021-05-13T03:16:53.003Z [test] third\n
func NewFormatWriter(dest io.Writer, serviceName string) io.Writer {
	return &formatter{
		serviceName:    serviceName,
		dest:           dest,
		writeTimestamp: true,
	}
}

type trimWriter struct {
	dest     io.Writer
	re       *regexp.Regexp
	buf      []byte
	postTrim bool
}

func NewTrimWriter(dest io.Writer, regex string) (io.Writer, error) {
	re, err := regexp.Compile(regex)
	if err != nil {
		return nil, err
	}
	return &trimWriter{
		dest: dest,
		re:   re,
	}, nil
}

func (w *trimWriter) Write(p []byte) (nn int, ee error) {
	pos := len(w.buf)
	w.buf = append(w.buf, p...)
	written := 0
	for {
		end := bytes.IndexRune(w.buf[pos:], '\n') + pos
		if end == -1 {
			return written, nil // wait for rest of line
		}
		loc := w.re.FindIndex(w.buf[:end])
		start := 0
		if loc != nil && loc[0] == 0 {
			start = loc[1]
		}

		write := w.buf[start : end+1]
		n, err := w.dest.Write(write)
		written += n
		w.buf = w.buf[end+1:]
		if err != nil {
			return written, err
		}
		pos = 0
	}
	return written, nil
}

func (w *trimWriter) Write2(p []byte) (nn int, ee error) {
	written := 0
	for len(p) > 0 {
		if !w.postTrim {
			// The remainder is explicitly returned by findPrefix because the
			// trimmed prefix may include buffered content from previous Write
			// calls so we can't just infer the remainder from n and p.
			remainder, n := w.findPrefix(p)
			written += n // skipped bytes count as written
			w.postTrim = n > 0
			if n > 0 {
				w.postTrim = true
				p = remainder
			}
		}

		length := bytes.IndexRune(p, '\n')
		if !w.postTrim && length == -1 {
			return written, nil // pre-trim with no line end - wait for more data
		}
		if length == -1 {
			length = len(p) // post-trim with no line end - write entire buffer.
		}

		write := p[:length]
		n, err := w.dest.Write(write)
		w.postTrim = !bytes.ContainsRune(write, '\n')
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func (w *trimWriter) findPrefix(p []byte) (remainder []byte, n int) {
	w.buf = append(w.buf, p...)

	loc := w.re.FindIndex(w.buf)
	if loc == nil || loc[0] != 0 {
		// Found no match.  Matches must start at the beginning of a line
		return nil, 0
	} else if loc[1] == len(w.buf)-1 {
		// Match may be longer than current buffer content, wait for more
		return nil, 0
	}

	remainder = w.buf[loc[1]:]
	w.buf = w.buf[:0]
	return remainder, loc[1] - loc[0]
}

func (f *formatter) Write(p []byte) (nn int, ee error) {
	f.mut.Lock()
	defer f.mut.Unlock()
	written := 0
	for len(p) > 0 {
		if f.writeTimestamp {
			f.writeTimestamp = false
			f.timestampBuffer = time.Now().UTC().AppendFormat(f.timestampBuffer[:0], outputTimeFormat)
			f.timestampBuffer = append(f.timestampBuffer, " ["...)
			f.timestampBuffer = append(f.timestampBuffer, f.serviceName...)
			f.timestampBuffer = append(f.timestampBuffer, "] "...)
			f.timestamp = f.timestampBuffer
		}

		for len(f.timestamp) > 0 {
			// Timestamp bytes don't count towards the returned count because they constitute the
			// encoding not the payload.
			n, err := f.dest.Write(f.timestamp)
			f.timestamp = f.timestamp[n:]
			if err != nil {
				return written, err
			}
		}

		length := 0
		for i := 0; i < len(p); i++ {
			length++
			if p[i] == '\n' {
				f.writeTimestamp = true
				break
			}
		}

		write := p[:length]
		n, err := f.dest.Write(write)
		p = p[n:]
		written += n
		if err != nil {
			return written, err
		}
	}
	return written, nil
}
