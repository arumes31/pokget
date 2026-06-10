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

import "time"

type PortfolioItem struct {
	ID             string    `json:"id"`
	UserID         string    `json:"user_id"`
	CardID         string    `json:"card_id"`
	Condition      string    `json:"condition"`
	Format         string    `json:"format"` // Raw, Graded
	Grade          string    `json:"grade"`
	GradingCompany string    `json:"grading_company"`
	Notes          string    `json:"notes"`
	IsPublic       bool      `json:"is_public"`
	CustomPrice    *float64  `json:"custom_price"`
	Language       string    `json:"language"`
	CreatedAt      time.Time `json:"created_at"`

	// Join fields
	Card Card `json:"card"`
}

type WantlistItem struct {
	ID          string    `json:"id"`
	UserID      string    `json:"user_id"`
	CardID      string    `json:"card_id"`
	TargetPrice float64   `json:"target_price"`
	Notes       string    `json:"notes"`
	CreatedAt   time.Time `json:"created_at"`

	// Join fields
	Card Card `json:"card"`
}

type ErrorCard struct {
	ID                       string    `json:"id"`
	CardID                   string    `json:"card_id"`
	ErrorType                string    `json:"error_type"`
	Description              string    `json:"description"`
	EstimatedValueMultiplier float64   `json:"estimated_value_multiplier"`
	SubmittedBy              string    `json:"submitted_by"`
	ImageURL                 string    `json:"image_url"`
	CreatedAt                time.Time `json:"created_at"`

	// Join fields
	Card Card `json:"card"`
}

func (p PortfolioItem) GetCustomPrice() float64 {
	if p.CustomPrice == nil {
		return 0
	}
	return *p.CustomPrice
}

