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
	"errors"
	"io"
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

type timeTrimWriter struct {
	dest     io.Writer
	format   string
	buf      []byte
	postTime bool
}

func NewTimeTrimWriter(dest io.Writer, format string) io.Writer {
	return &timeTrimWriter{
		format: format,
		dest:   dest,
	}
}

func (w *timeTrimWriter) writePreTime(p []byte) (remainder []byte, nn int, ee error) {
	w.buf = append(w.buf, p...)
	_, n, err := parseTimePrefix(w.buf, w.format)
	if err != nil {
		// haven't received entire time prefix yet - wait for more
		return nil, 0, nil
	}

	remainder = w.buf[n:]
	w.buf = w.buf[:0]
	return remainder, n, nil
}

func (w *timeTrimWriter) Write(p []byte) (nn int, ee error) {
	written := 0
	for len(p) > 0 {
		if !w.postTime {
			remainder, n, err := w.writePreTime(p)
			written += n
			if err != nil {
				return written, err
			}
			w.postTime = n > 0
			p = remainder
		}

		length := 0
		for i := 0; i < len(p); i++ {
			length++
			if p[i] == '\n' {
				break
			}
		}

		write := p[:length]
		n, err := w.dest.Write(write)
		w.postTime = !bytes.ContainsRune(write, '\n')
		written += n
		p = p[n:]
		if err != nil {
			return written, err
		}
	}
	return written, nil
}

func parseTimePrefix(buf []byte, format string) (time.Time, int, error) {
	for n := 0; n <= len(buf); n++ {
		t, err := time.Parse(format, string(buf[:n]))
		if err != nil {
			continue
		}
		return t, n, nil
	}
	return time.Time{}, 0, errors.New("no time prefix found")
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
