package source

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestFFTCGProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/en/get-cards" {
			http.NotFound(w, r)
			return
		}
		if r.Method != http.MethodPost {
			http.Error(w, "expected POST", http.StatusMethodNotAllowed)
			return
		}
		_, _ = w.Write([]byte(`{
			"cards": [
				{
					"id": 1,
					"code": "1-001H",
					"image": "{\"1\": {\"alt\": \"1-001H\", \"title\": \"1-001H\", \"filename\": \"images/cards/full/1-001H_eg.jpg\"}}",
					"element": ["火"],
					"rarity": "H",
					"cost": "6",
					"power": "9000",
					"set": ["Opus I"],
					"name_en": "Auron",
					"name_ja": "アーロン",
					"name_de": "Auron",
					"type_en": "Forward"
				}
			],
			"count": 1
		}`))
	}))
	defer server.Close()

	provider := &FFTCGProvider{
		HTTP:         testHTTP(server),
		BaseURL:      server.URL,
		ImageBaseURL: "https://fftcg.cdn.sewest.net",
		Language:     "en",
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Auron" || records[0].CollectorNumber != "1-001H" || records[0].SetCode != "Opus 1" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "Hero" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != "https://fftcg.cdn.sewest.net/images/cards/full/1-001H_eg.jpg" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected result count: %d", result.Count)
	}
}

func TestGodzillaProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/manage/card-list-user/list" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"page": 1,
			"per_page": 100,
			"total": 1,
			"items": [
				{
					"id": 877,
					"picture": "SCS01/ESC01-002_ol.png",
					"card_number": "ESC01-002",
					"card_name": "Godzilla(1995)",
					"card_kind": "Monster",
					"rare": "SR",
					"expansion": "SCS01",
					"expansion_name": "Sleeve & Card Set Ink-Brushed Monster Rampage",
					"color": "Blue",
					"text": "Play up to 1 rank 3 or lower battle card"
				}
			]
		}`))
	}))
	defer server.Close()

	provider := &GodzillaProvider{
		HTTP:         testHTTP(server),
		BaseURL:      server.URL,
		ImageBaseURL: "https://en.godzilla-cardgame.com/wordpress/wp-content/images/cardlist",
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Godzilla(1995)" || records[0].CollectorNumber != "ESC01-002" || records[0].SetCode != "SCS01" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "SR" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != "https://en.godzilla-cardgame.com/wordpress/wp-content/images/cardlist/SCS01/ESC01-002_ol.png" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected result count: %d", result.Count)
	}
}

func TestShadowverseProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cards/searchresults/":
			_, _ = w.Write([]byte(`
				<html>
				<script>var max_page = 1;</script>
				<ul class="cardlist-Result_List">
					<li class="ex-item">
						<a href="/cards/?cardno=BP20-008EN&view=text">
							<div class="img w100"><img src="/wordpress/wp-content/images/cardlist/BP20/BP20-008EN.png" alt="Eradicating Arrow" title="Eradicating Arrow"></div>
							<div class="center-Txtarea txt">
								<p class="number">BP20-008EN</p>
								<p class="ttl">Eradicating Arrow</p>
								<div class="status"><span class="status-Item">Spell</span><span class="status-Item">Gold</span></div>
							</div>
						</a>
					</li>
				</ul>
				</html>
			`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &ShadowverseEvolveProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL,
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Eradicating Arrow" || records[0].CollectorNumber != "BP20-008EN" || records[0].SetCode != "BP20" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "Gold" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != server.URL+"/wordpress/wp-content/images/cardlist/BP20/BP20-008EN.png" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}

func TestUniVersusProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/listing_cards.php" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`
			<div class="debug">1 cartes</div>
			<div class="card even">
				<div onmouseover="preview2('ft01', '051', 'attack');">
					<div class="card_infos">
						<div class="card_title">
							<h1>Iron Dragon's Scales</h1>
							<div class="card-important-info">
								<span class="label lc-label card-list-attack">Attack</span><br />
								Common
							</div>
						</div>
					</div>
				</div>
			</div>
		`))
	}))
	defer server.Close()

	provider := &UniVersusProvider{
		HTTP:         testHTTP(server),
		BaseURL:      server.URL,
		ImageBaseURL: "https://uvsultra.online/images/extensions",
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Iron Dragon's Scales" || records[0].CollectorNumber != "051" || records[0].SetCode != "FT01" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "Common" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != "https://uvsultra.online/images/extensions/ft01/051-preview.jpg" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}

func TestVanguardProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cardlist/cardsearch/":
			_, _ = w.Write([]byte(`
				<html>
				<script>var max_page = 1;</script>
				<ul>
					<li class="ex-item">
						<a href="/cardlist/?cardno=BCS2022/VGV01EN&view=text">
							<div class="image">
								<img src="/wordpress/wp-content/images/cardlist/bcs2022/bcs2022_vgv01.png" class="cardimage" alt="Nightmare Doll, Tosca">
							</div>
							<div class="text">
								<div class="number">BCS2022/VGV01EN</div>
								<h5>Nightmare Doll, Tosca</h5>
								<div class="status">Normal Unit｜Pale Moon｜Grade 1｜RRR</div>
							</div>
						</a>
					</li>
				</ul>
				</html>
			`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &VanguardProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL,
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Nightmare Doll, Tosca" || records[0].CollectorNumber != "BCS2022/VGV01EN" || records[0].SetCode != "BCS2022" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "RRR" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != server.URL+"/wordpress/wp-content/images/cardlist/bcs2022/bcs2022_vgv01.png" {
		t.Fatalf("unexpected image: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}

func TestHololiveProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/cardlist/cardsearch/":
			_, _ = w.Write([]byte(`
				<html>
				<script>var max_page = 1;</script>
				<ul>
					<li class="ex-item">
						<a href="/cardlist/?id=3041&view=text">
							<div class="img w100"><img src="/wp-content/images/cardlist/hSD20/hSD20-014_U.png" alt="Megaphone"></div>
							<div class="center-Txtarea txt">
								<p class="number">hSD20-014</p>
								<p class="name">百灯キョーコの拡声器</p>
								<div class="info">
									<dl>
										<dt>レアリティ</dt>
										<dd>U</dd>
									</dl>
								</div>
							</div>
						</a>
					</li>
				</ul>
				</html>
			`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := &HololiveProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL,
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "百灯キョーコの拡声器" || records[0].CollectorNumber != "hSD20-014" || records[0].SetCode != "hSD20" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "U" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != server.URL+"/wp-content/images/cardlist/hSD20/hSD20-014_U.png" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}

func TestDuelMastersProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/DuelMastersCards.json" {
			http.NotFound(w, r)
			return
		}
		_, _ = w.Write([]byte(`{
			"cards": [
				{
					"name": "Abduction Charger",
					"cost": 7,
					"civilizations": ["Water"],
					"type": "Spell",
					"text": "Choose up to 2 creatures",
					"printings": [
						{
							"set": "DM-09 Fatal Brood",
							"id": "16/55",
							"rarity": "Rare"
						}
					]
				}
			]
		}`))
	}))
	defer server.Close()

	provider := &DuelMastersProvider{
		HTTP:    testHTTP(server),
		BaseURL: server.URL,
	}

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Abduction Charger" || records[0].CollectorNumber != "16/55" || records[0].SetCode != "DM-09" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "Rare" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}

func TestTCGCSVProvider(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/90/groups":
			_, _ = w.Write([]byte(`{"results": [{"groupId": 24886, "name": "Festival Arena", "abbreviation": "BS12"}]}`))
		case "/90/24886/products":
			_, _ = w.Write([]byte(`{"results": [
				{
					"productId": 717968,
					"name": "Langue de Chat Cookie",
					"cleanName": "Langue de Chat Cookie",
					"imageUrl": "https://tcgplayer-cdn.tcgplayer.com/product/717968_200w.jpg",
					"groupId": 24886,
					"extendedData": [
						{"name": "Rarity", "value": "C"},
						{"name": "Number", "value": "BS12-001"}
					]
				}
			]}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	provider := NewCookieRunProvider(testHTTP(server))
	provider.BaseURL = server.URL

	records, result := fetchRecords(t, provider)
	if len(records) != 1 {
		t.Fatalf("expected 1 record, got %d", len(records))
	}
	if records[0].Name != "Langue de Chat Cookie" || records[0].CollectorNumber != "BS12-001" || records[0].SetCode != "BS12" {
		t.Fatalf("unexpected record: %+v", records[0])
	}
	if records[0].Rarity != "C" {
		t.Fatalf("unexpected rarity: %q", records[0].Rarity)
	}
	if len(records[0].Images) != 1 || records[0].Images[0].URL != "https://tcgplayer-cdn.tcgplayer.com/product/717968_200w.jpg" {
		t.Fatalf("unexpected images: %+v", records[0].Images)
	}
	if result.Count != 1 {
		t.Fatalf("unexpected count: %d", result.Count)
	}
}
