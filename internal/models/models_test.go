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

package models

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestCardInstantiation(t *testing.T) {
	price, _ := decimal.NewFromString("10.50")
	c := Card{
		ID:        "123",
		Name:      "Pikachu",
		Set:       "Base Set",
		PriceUSD:  price,
		PriceEUR:  price,
		ImageURL:  "http://example.com/pikachu.png",
		Change24h: 5.5,
		Variant:   "Holo",
		Language:  "en",
	}

	if c.Name != "Pikachu" {
		t.Errorf("Expected name Pikachu, got %s", c.Name)
	}

	// Test JSON serialization to ensure tags are somewhat correct
	data, err := json.Marshal(c)
	if err != nil {
		t.Errorf("Failed to marshal card: %v", err)
	}
	if len(data) == 0 {
		t.Error("Empty JSON data")
	}
}

func TestUserInstantiation(t *testing.T) {
	now := time.Now()
	u := User{
		ID:                "u123",
		Email:             "test@example.com",
		PasswordHash:      "hash",
		IsVerified:        true,
		VerificationToken: "token",
		CreatedAt:         now,
	}

	if u.Email != "test@example.com" {
		t.Errorf("Expected email test@example.com, got %s", u.Email)
	}

	data, err := json.Marshal(u)
	if err != nil {
		t.Errorf("Failed to marshal user: %v", err)
	}
	
	// PasswordHash and VerificationToken should be ignored in JSON
	var unmarshaled map[string]interface{}
	_ = json.Unmarshal(data, &unmarshaled)
	
	if _, ok := unmarshaled["PasswordHash"]; ok {
		t.Error("PasswordHash should not be serialized")
	}
	if _, ok := unmarshaled["VerificationToken"]; ok {
		t.Error("VerificationToken should not be serialized")
	}
}
