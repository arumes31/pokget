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
	"os"
	"testing"
)

func TestConfigLoad(t *testing.T) {
	// Set dummy env
	os.Setenv("APP_PORT", "9090")
	os.Setenv("DB_HOST", "db")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "u")
	os.Setenv("DB_PASSWORD", "p")
	os.Setenv("DB_NAME", "n")
	os.Setenv("SESSION_KEY", "12345678901234567890123456789012")
	defer os.Unsetenv("APP_PORT")
	defer os.Unsetenv("DB_HOST")
	defer os.Unsetenv("DB_PORT")
	defer os.Unsetenv("DB_USER")
	defer os.Unsetenv("DB_PASSWORD")
	defer os.Unsetenv("DB_NAME")
	defer os.Unsetenv("SESSION_KEY")

	cfg, err := Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.App.Port != "9090" {
		t.Errorf("Expected port 9090, got %s", cfg.App.Port)
	}
	if cfg.DB.Host != "db" {
		t.Errorf("Expected db host db, got %s", cfg.DB.Host)
	}
}

func TestConfigLoad_Error(t *testing.T) {
	// SESSION_KEY is required but we unset it
	os.Setenv("APP_PORT", "8080")
	os.Setenv("DB_HOST", "localhost")
	os.Setenv("DB_PORT", "5432")
	os.Setenv("DB_USER", "u")
	os.Setenv("DB_PASSWORD", "p")
	os.Setenv("DB_NAME", "n")
	os.Unsetenv("SESSION_KEY")
	
	_, err := Load()
	if err == nil {
		t.Error("Expected error when SESSION_KEY is missing")
	}
}
