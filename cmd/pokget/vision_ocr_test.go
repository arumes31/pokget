package main

import (
	"testing"

	"pokget/internal/config"
	"pokget/internal/service"
)

func TestConfigureVisionOCR(t *testing.T) {
	for _, tc := range []struct {
		name             string
		enabled          bool
		url              string
		seconds, threads int
		wantError        bool
	}{
		{name: "disabled ignores unused settings", url: "invalid"},
		{name: "enabled", enabled: true, url: "http://localhost:11434", seconds: 45, threads: 4},
		{name: "invalid URL", enabled: true, url: "invalid", seconds: 45, threads: 4, wantError: true},
		{name: "invalid timeout", enabled: true, url: "http://localhost:11434", seconds: -1, threads: 4, wantError: true},
		{name: "excessive timeout", enabled: true, url: "http://localhost:11434", seconds: 46, threads: 4, wantError: true},
		{name: "invalid threads", enabled: true, url: "http://localhost:11434", seconds: 45, threads: -1, wantError: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.Config
			cfg.VisionOCR.Enabled = tc.enabled
			cfg.VisionOCR.BaseURL = tc.url
			cfg.VisionOCR.TimeoutSeconds = tc.seconds
			cfg.VisionOCR.Threads = tc.threads
			p := service.NewDetectionPipeline(nil, nil)
			err := configureVisionOCR(&cfg, p)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
			}
			if !tc.wantError && (p.VisionOCR != nil) != tc.enabled {
				t.Fatal("enabled setting not applied")
			}
		})
	}
}
