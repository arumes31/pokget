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

package config

import (
	"fmt"

	"github.com/ilyakaznacheev/cleanenv"
)

type Config struct {
	App struct {
		Name          string `env:"APP_NAME" env-default:"Pokget"`
		Port          string `env:"APP_PORT" env-default:"18066"`
		Debug         bool   `env:"DEBUG" env-default:"false"`
		SecureCookies bool   `env:"SECURE_COOKIES" env-default:"true"` // BUG-C03: Configurable Secure flag for session cookies
		WriteTimeout  int    `env:"WRITE_TIMEOUT" env-default:"120"`   // BUG-C05: Configurable write timeout in seconds
	} `yaml:"app"`
	DB struct {
		Host           string `env:"DB_HOST" env-required:"true"`
		Port           string `env:"DB_PORT" env-required:"true"`
		User           string `env:"DB_USER" env-required:"true"`
		Password       string `env:"DB_PASSWORD" env-required:"true"`
		Name           string `env:"DB_NAME" env-required:"true"`
		SSLMode        string `env:"DB_SSLMODE" env-default:"prefer"`
		MigrationsPath string `env:"MIGRATIONS_PATH" env-default:"migrations"`
	} `yaml:"db"`
	Redis struct {
		Host     string `env:"REDIS_HOST" env-default:"localhost"`
		Port     string `env:"REDIS_PORT" env-default:"6379"`
		Password string `env:"REDIS_PASSWORD"`
	} `yaml:"redis"`
	SMTP struct {
		Host string `env:"SMTP_HOST"`
		Port string `env:"SMTP_PORT"`
		User string `env:"SMTP_USER"`
		Pass string `env:"SMTP_PASS"`
		From string `env:"SMTP_FROM"`
	} `yaml:"smtp"`
	Auth struct {
		SessionKey string `env:"SESSION_KEY" env-required:"true"`
	} `yaml:"auth"`
	Scan struct {
		PhashHighConf  int `env:"SCAN_PHASH_HIGH_CONF" env-default:"5"`  // SCAN-02: Strict pHash threshold
		PhashPotential int `env:"SCAN_PHASH_POTENTIAL" env-default:"10"` // SCAN-02: Relaxed pHash threshold
		OCRPoolSize    int `env:"SCAN_OCR_POOL_SIZE" env-default:"3"`    // SCAN-03: Number of concurrent Tesseract clients
		TimeoutSeconds int `env:"SCAN_TIMEOUT_SECONDS" env-default:"75"`
	} `yaml:"scan"`
	Catalog struct {
		Enabled             bool   `env:"CATALOG_ENABLED" env-default:"true"`
		LegacyMetadataSync  bool   `env:"LEGACY_METADATA_SYNC_ENABLED" env-default:"false"`
		Language            string `env:"CATALOG_LANGUAGE" env-default:"en"`
		SyncIntervalMins    int    `env:"CATALOG_SYNC_INTERVAL_MINUTES" env-default:"360"`
		BatchSize           int    `env:"CATALOG_BATCH_SIZE" env-default:"500"`
		RequestDelayMS      int    `env:"CATALOG_REQUEST_DELAY_MS" env-default:"100"`
		WeissMaxPages       int    `env:"CATALOG_WEISS_MAX_PAGES" env-default:"0"`
		ImagesEnabled       bool   `env:"CATALOG_IMAGES_ENABLED" env-default:"true"`
		ImageStore          string `env:"CATALOG_IMAGE_STORE" env-default:"data/catalog-images"`
		ImageBatchSize      int    `env:"CATALOG_IMAGE_BATCH_SIZE" env-default:"8"`
		ImagePollIntervalMS int    `env:"CATALOG_IMAGE_POLL_INTERVAL_MS" env-default:"5000"`
	} `yaml:"catalog"`
	Worker struct {
		PriceSyncMinutes     int     `env:"PRICE_SYNC_INTERVAL_MINUTES" env-default:"60"`
		MetadataTargets      string  `env:"METADATA_TARGETS" env-default:"pokemon:en"`
		RequestsPerSecond    float64 `env:"WORKER_REQUESTS_PER_SECOND" env-default:"0.5"`
		RequestBurst         int     `env:"WORKER_REQUEST_BURST" env-default:"1"`
		RetryAttempts        int     `env:"WORKER_RETRY_ATTEMPTS" env-default:"3"`
		RetryBaseDelayMS     int     `env:"WORKER_RETRY_BASE_DELAY_MS" env-default:"1000"`
		CircuitFailures      int     `env:"WORKER_CIRCUIT_FAILURES" env-default:"3"`
		CircuitCooldownSec   int     `env:"WORKER_CIRCUIT_COOLDOWN_SECONDS" env-default:"300"`
		MaxPriceRatio        float64 `env:"WORKER_MAX_PRICE_RATIO" env-default:"5"`
		HistoryRetentionDays int     `env:"PRICE_HISTORY_RETENTION_DAYS" env-default:"730"`
		FailurePath          string  `env:"WORKER_FAILURE_PATH" env-default:"data/worker-failures.jsonl"`
	} `yaml:"worker"`
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	return &cfg, nil
}
