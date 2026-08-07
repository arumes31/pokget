// Copyright (c) 2026 arumes31
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package main

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"strings"
	"testing"
)

func TestNewLogHandler(t *testing.T) {
	tests := []struct {
		name       string
		format     string
		expectJSON bool
	}{
		{name: "default text", format: "text"},
		{name: "json opt in", format: "json", expectJSON: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(newLogHandler(&output, test.format, nil))
			logger.Info("scan started", "game", "pokemon")

			line := strings.TrimSpace(output.String())
			if strings.Count(line, "\n") != 0 {
				t.Fatalf("log output contains multiple lines: %q", line)
			}
			if test.expectJSON {
				var record map[string]any
				if err := json.Unmarshal([]byte(line), &record); err != nil {
					t.Fatalf("JSON log output is invalid: %v; output=%q", err, line)
				}
				if record["msg"] != "scan started" || record["game"] != "pokemon" {
					t.Fatalf("JSON log record = %#v", record)
				}
				return
			}

			if !strings.Contains(line, "level=INFO") ||
				!strings.Contains(line, `msg="scan started"`) ||
				!strings.Contains(line, "game=pokemon") {
				t.Fatalf("text log output = %q", line)
			}
		})
	}
}
