package main

import (
	"errors"
	"time"

	"pokget/internal/config"
	"pokget/internal/service"
	"pokget/internal/visionocr"
)

// Configuring the provider makes no requests and never downloads a model.
func configureVisionOCR(cfg *config.Config, pipeline *service.DetectionPipeline) error {
	if !cfg.VisionOCR.Enabled {
		return nil
	}
	if cfg.VisionOCR.TimeoutSeconds < 1 || cfg.VisionOCR.TimeoutSeconds > 45 || cfg.VisionOCR.Threads < 1 {
		return errors.New("vision OCR: SCAN_VISION_OCR_TIMEOUT_SECONDS must be 1..45 and SCAN_VISION_OCR_THREADS must be positive")
	}
	client, err := visionocr.New(visionocr.Config{
		BaseURL: cfg.VisionOCR.BaseURL, Model: cfg.VisionOCR.Model,
		Timeout: time.Duration(cfg.VisionOCR.TimeoutSeconds) * time.Second, Threads: cfg.VisionOCR.Threads,
	})
	if err != nil {
		return err
	}
	pipeline.VisionOCR = client
	return nil
}
