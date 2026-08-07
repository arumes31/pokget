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
	"context"
	"log/slog"
	"strings"
	"testing"
	"time"

	"pokget/internal/catalog"
)

type fingerprintProgressSourceStub struct {
	samples []catalog.ImageProgress
	next    int
}

func (s *fingerprintProgressSourceStub) ImageProgress(context.Context) (catalog.ImageProgress, error) {
	sample := s.samples[s.next]
	s.next++
	return sample, nil
}

func TestFingerprintProgressReporterLogsQueueStatusAndRate(t *testing.T) {
	start := time.Date(2026, time.August, 8, 12, 0, 0, 0, time.UTC)
	source := &fingerprintProgressSourceStub{samples: []catalog.ImageProgress{
		{Total: 100, Ready: 25, Pending: 75, Fingerprints: 75},
		{Total: 100, Ready: 35, Pending: 65, Fingerprints: 105},
	}}
	var output bytes.Buffer
	reporter := newFingerprintProgressReporter(source, slog.New(slog.NewTextHandler(&output, nil)))
	reporter.now = func() time.Time { return start }

	if err := reporter.Report(context.Background()); err != nil {
		t.Fatal(err)
	}
	reporter.now = func() time.Time { return start.Add(time.Minute) }
	if err := reporter.Report(context.Background()); err != nil {
		t.Fatal(err)
	}

	lines := strings.Split(strings.TrimSpace(output.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("log lines = %d, want 2: %q", len(lines), output.String())
	}
	for _, field := range []string{
		"status=running", "total_images=100", "ready_images=35", "remaining_images=65",
		"fingerprints=105", "ready_percent=35", "generated_since_last=10",
		"rate_per_minute=10", "eta=6m30s",
	} {
		if !strings.Contains(lines[1], field) {
			t.Errorf("second progress log missing %q: %s", field, lines[1])
		}
	}
}
