package service

import "testing"

func TestPrintedCollectorFractions(t *testing.T) {
	for _, test := range []struct {
		text, collector string
		want            bool
	}{
		{"136/189", "136", true}, {"001 / 189", "1", true},
		{"TG01/TG30", "tg1", true}, {"71.6 lbs 4 Draw 3", "4", false},
		{"136/189", "13", false}, {"136/189", "189", false},
		{"1/2", "1", false}, {"203/198", "203", true},
		{"TG01/GG30", "tg1", false}, {"x136/189x", "136", false},
	} {
		if got := printedCollectorFractions(test.text)[collectorNumberKey(test.collector)]; got != test.want {
			t.Errorf("%q collector %q = %v, want %v", test.text, test.collector, got, test.want)
		}
	}
}
