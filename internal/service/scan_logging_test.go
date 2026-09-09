package service

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"testing"
)

func TestScanStageLogsCorrelatedOutcomesWithoutErrorContents(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		outcome string
	}{
		{"success", nil, "complete"}, {"timeout", context.DeadlineExceeded, "timeout"},
		{"cancel", context.Canceled, "cancelled"}, {"failure", errors.New("PRIVATE provider response"), "failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var output bytes.Buffer
			logger := slog.New(slog.NewJSONHandler(&output, nil)).With("scan_id", "test-scan")
			ctx := WithScanLogger(context.Background(), logger)
			finish := LogScanStage(ctx, "server_ocr")
			if !strings.Contains(output.String(), "Scan stage started") {
				t.Fatal("no live start event")
			}
			finish(tc.err)
			for _, field := range []string{`"scan_id":"test-scan"`, `"stage":"server_ocr"`, `"duration_ms":`, `"outcome":"` + tc.outcome + `"`} {
				if !strings.Contains(output.String(), field) {
					t.Errorf("missing %s", field)
				}
			}
			if strings.Contains(output.String(), "PRIVATE") {
				t.Error("raw error content logged")
			}
		})
	}
}
