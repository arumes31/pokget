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

package service

import "testing"

func TestParseCardmarketPrice(t *testing.T) {
	tests := []struct {
		name    string
		input   string
		want    float64
		wantErr bool
	}{
		{"simple decimal", "12,34 €", 12.34, false},
		{"sub-euro", "0,30 €", 0.30, false},
		{"trailing zeros", "10,00 €", 10.0, false},
		{"thousands separator", "1.234,56 €", 1234.56, false},
		{"millions", "1.000.000,00 €", 1000000.0, false},
		{"no currency symbol", "5,99", 5.99, false},
		{"leading/trailing space", "  7,50 € ", 7.50, false},
		{"non-breaking space", "3,20\u00A0€", 3.20, false},
		{"german thousands no comma", "1.234 €", 1234.0, false},
		{"german large thousands no comma", "12.345 €", 12345.0, false},
		{"invalid text", "invalid", 0, true},
		{"empty", "", 0, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseCardmarketPrice(tt.input)
			if tt.wantErr {
				if err == nil {
					t.Fatalf("expected error for %q, got %v", tt.input, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("unexpected error for %q: %v", tt.input, err)
			}
			if got != tt.want {
				t.Errorf("parseCardmarketPrice(%q) = %v, want %v", tt.input, got, tt.want)
			}
		})
	}
}
