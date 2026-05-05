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
		Name  string `env:"APP_NAME" env-default:"Pokget"`
		Port  string `env:"APP_PORT" env-default:"8080"`
		Debug bool   `env:"DEBUG" env-default:"false"`
	}
	DB struct {
		Host     string `env:"DB_HOST" env-required:"true"`
		Port     string `env:"DB_PORT" env-required:"true"`
		User     string `env:"DB_USER" env-required:"true"`
		Password string `env:"DB_PASSWORD" env-required:"true"`
		Name     string `env:"DB_NAME" env-required:"true"`
		SSLMode  string `env:"DB_SSLMODE" env-default:"disable"`
	}
	Redis struct {
		Host     string `env:"REDIS_HOST" env-default:"localhost"`
		Port     string `env:"REDIS_PORT" env-default:"6379"`
		Password string `env:"REDIS_PASSWORD"`
	}
	SMTP struct {
		Host     string `env:"SMTP_HOST"`
		Port     string `env:"SMTP_PORT"`
		User     string `env:"SMTP_USER"`
		Pass     string `env:"SMTP_PASS"`
		From     string `env:"SMTP_FROM"`
	}
	Auth struct {
		SessionKey string `env:"SESSION_KEY" env-required:"true"`
	}
}

func Load() (*Config, error) {
	var cfg Config
	if err := cleanenv.ReadEnv(&cfg); err != nil {
		return nil, fmt.Errorf("config error: %w", err)
	}
	return &cfg, nil
}
