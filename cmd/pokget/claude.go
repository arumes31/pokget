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
	"errors"
	"log/slog"
	"time"

	"pokget/internal/claudedetect"
	"pokget/internal/config"
	"pokget/internal/service"
)

// configureClaudeDetector instantiates the Claude card detection client and
// sets it as the primary detection provider on the detection pipeline.
func configureClaudeDetector(cfg *config.Config, pipeline *service.DetectionPipeline) error {
	if pipeline == nil {
		return nil
	}
	if !cfg.Claude.Enabled {
		slog.Info("Claude detector disabled in configuration")
		return nil
	}
	if cfg.Claude.APIKey == "" {
		return errors.New("claude detector: CLAUDE_API_KEY must not be empty when enabled")
	}
	timeout := time.Duration(cfg.Claude.TimeoutSeconds) * time.Second
	if timeout <= 0 {
		timeout = 30 * time.Second
	}
	client, err := claudedetect.New(claudedetect.Config{
		APIKey:        cfg.Claude.APIKey,
		Model:         cfg.Claude.Model,
		BaseURL:       cfg.Claude.BaseURL,
		Timeout:       timeout,
		PromptCaching: cfg.Claude.PromptCaching,
	})
	if err != nil {
		return err
	}
	pipeline.Claude = client
	slog.Info("Claude Card Detector configured as primary detector",
		"model", client.Model(),
		"prompt_caching", client.PromptCachingEnabled())
	return nil
}
