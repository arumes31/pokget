package source

import (
	"testing"

	"pokget/internal/catalog"
)

func TestTCGCSVVariantDerivesFinish(t *testing.T) {
	cases := []struct {
		game   catalog.Game
		name   string
		number string
		want   string
	}{
		{catalog.GameWIXOSS, "Sashe Memoria Natural Planet Parallel Foil", "WXDi-P06-044P[EN]", "Parallel Foil"},
		{catalog.GameWIXOSS, "Sashe Memoria Natural Planet", "WXDi-P06-044[EN]", ""},
		{catalog.GameRiftbound, "Rumble Hotheaded Alternate Art", "026a/221", "Alternate Art"},
		{catalog.GameRiftbound, "Rumble Hotheaded", "026/221", ""},
		{catalog.GameCookieRun, "Blue Slushy Cookie", "BS6-007", ""},
	}
	for _, tc := range cases {
		if got := tcgcsvVariant(tc.game, tc.name, tc.number); got != tc.want {
			t.Errorf("tcgcsvVariant(%s, %q, %q) = %q, want %q", tc.game, tc.name, tc.number, got, tc.want)
		}
	}
}
