package service

import "testing"

func TestNameRoleEvidence(t *testing.T) {
	tests := []struct {
		name          string
		text          string
		candidate     string
		wantText      string
		referenceOnly bool
	}{
		{"spaced evolution", "furret evolves from sentret hp 110", "sentret", "furret evolves from hp 110", true},
		{"compact marker", "furret evolvesfrom sentret", "sentret", "furret evolvesfrom", true},
		{"fully compact", "furret evolvesfromsentret hp 110", "sentret", "furret evolvesfrom hp 110", true},
		{"fused from", "furret evolves fromsentret", "sentret", "furret evolves from", true},
		{"repeated references", "evolves from sentret evolvesfromsentret", "sentret", "evolves from evolvesfrom", true},
		{"independent title", "sentret evolves from sentret", "sentret", "sentret evolves from", false},
		{"independent body", "furret evolves from sentret search for sentret", "sentret", "furret evolves from search for sentret", false},
		{"multiword", "evolves from galarian mr mime hp 100", "galarian mr mime", "evolves from hp 100", true},
		{"compact multiword", "evolvesfromgalarianmrmime hp 100", "galarian mr mime", "evolvesfrom hp 100", true},
		{"partially compact multiword", "evolves fromgalarian mr mime", "galarian mr mime", "evolves from", true},
		{"independent compact multiword", "galarianmrmime evolves from galarian mr mime", "galarian mr mime", "galarianmrmime evolves from", false},
		{"unicode name", "evolvesfromニャース hp 100", "ニャース", "evolvesfrom hp 100", true},
		{"marker word boundary", "devolvesfromsentret", "sentret", "devolvesfromsentret", false},
		{"name trailing boundary", "evolves from sentretish", "sentret", "evolves from sentretish", false},
		{"name numeric boundary", "evolvesfromsentret2", "sentret", "evolvesfromsentret2", false},
		{"name leading boundary", "evolves from resentret", "sentret", "evolves from resentret", false},
		{"multiword trailing boundary", "evolves from mr mimejr", "mr mime", "evolves from mr mimejr", false},
		{"not immediate", "evolves from the sentret", "sentret", "evolves from the sentret", false},
		{"ordinary title", "sentret hp 70", "sentret", "sentret hp 70", false},
		{"unrelated evolution", "sentret evolves from pikachu", "sentret", "sentret evolves from pikachu", false},
		{"empty name", "evolves from sentret", "", "evolves from sentret", false},
		{"empty text", "", "sentret", "", false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotText, gotReferenceOnly := nameRoleEvidence(tt.text, tt.candidate)
			if gotText != tt.wantText || gotReferenceOnly != tt.referenceOnly {
				t.Fatalf("nameRoleEvidence(%q, %q) = (%q, %v), want (%q, %v)", tt.text, tt.candidate, gotText, gotReferenceOnly, tt.wantText, tt.referenceOnly)
			}
		})
	}
}
