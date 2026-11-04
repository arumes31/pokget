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

func TestTCGCSVImageURLOnlyUpgradesWixoss(t *testing.T) {
	const thumb = "https://tcgplayer-cdn.tcgplayer.com/product/1_200w.jpg"
	if got := tcgcsvImageURL(catalog.GameWIXOSS, thumb); got != "https://tcgplayer-cdn.tcgplayer.com/product/1_in_1000x1000.jpg" {
		t.Errorf("wixoss image = %q, want the 1000px scan", got)
	}
	for _, game := range []catalog.Game{catalog.GameCookieRun, catalog.GameForceOfWill, catalog.GameRiftbound, catalog.GameAlphaClash} {
		if got := tcgcsvImageURL(game, thumb); got != thumb {
			t.Errorf("%s image = %q, want the thumbnail kept", game, got)
		}
	}
}
