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
	"testing"

	"pokget/internal/config"
	"pokget/internal/service"
)

func TestConfigureClaudeDetector(t *testing.T) {
	tests := []struct {
		name      string
		enabled   bool
		apiKey    string
		model     string
		wantError bool
	}{
		{
			name:    "disabled",
			enabled: false,
		},
		{
			name:      "enabled without API key returns error",
			enabled:   true,
			apiKey:    "",
			wantError: true,
		},
		{
			name:      "enabled with API key succeeds and sets detector",
			enabled:   true,
			apiKey:    "sk-ant-test-key",
			model:     "claude-sonnet-5-5",
			wantError: false,
		},
		{
			name:      "enabled with opus model succeeds",
			enabled:   true,
			apiKey:    "sk-ant-test-key",
			model:     "claude-opus-5-5",
			wantError: false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var cfg config.Config
			cfg.Claude.Enabled = tc.enabled
			cfg.Claude.APIKey = tc.apiKey
			cfg.Claude.Model = tc.model
			cfg.Claude.TimeoutSeconds = 30
			cfg.Claude.PromptCaching = true

			pipeline := service.NewDetectionPipeline(nil, nil)
			err := configureClaudeDetector(&cfg, pipeline)
			if (err != nil) != tc.wantError {
				t.Fatalf("error=%v, wantError=%v", err, tc.wantError)
			}
			if !tc.wantError && tc.enabled && pipeline.Claude == nil {
				t.Fatal("expected pipeline.Claude to be configured")
			}
			if !tc.enabled && pipeline.Claude != nil {
				t.Fatal("expected pipeline.Claude to be nil when disabled")
			}
		})
	}
}
