package source

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestDBSMastersOfficialProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		category := r.URL.Query().Get("category")
		if category == "428001" {
			_, _ = w.Write([]byte(`
				<dl class="cardListCol">
					<dt class="cardNumber">BT1-001</dt>
					<dd class="cardName">Champa</dd>
					<dd class="leftCol">
						<div class="cardimg"><img src="../../images/cardimg/BT1-001.png" alt="Champa"></div>
						<dl class="seriesCol"><dt>Series</dt><dd>Series 1 Booster ~GALACTIC BATTLE~</dd></dl>
						<dl class="rarityCol"><dt>Rarity</dt><dd>Rare[R]</dd></dl>
					</dd>
				</dl>
				<dl class="cardListCol">
					<dt class="cardNumber">BT1-031_SPR</dt>
					<dd class="cardName">God Break Son Goku</dd>
					<dd class="leftCol">
						<div class="cardimg"><img src="../../images/cardimg/BT1-031_SPR.png" alt="God Break Son Goku"></div>
						<dl class="seriesCol"><dt>Series</dt><dd>Series 1 Booster ~GALACTIC BATTLE~</dd></dl>
						<dl class="rarityCol"><dt>Rarity</dt><dd>Special Rare[SPR]</dd></dl>
					</dd>
				</dl>
			`))
			return
		}
		// Root page with category select
		_, _ = w.Write([]byte(`
			<select name="category_exp">
				<option value="">Unspecified</option>
				<option value="428001">Series 1 Booster [BT1]</option>
			</select>
		`))
	}))
	defer server.Close()

	provider := &DBSMastersOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL}
	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("unexpected result: count=%d records=%d", result.Count, len(records))
	}

	// Verify normal card
	if records[0].SourceCardID != "BT1-001" || records[0].Name != "Champa" || records[0].SetCode != "BT1" {
		t.Fatalf("unexpected record 0: %+v", records[0])
	}
	if records[0].CollectorNumber != "BT1-001" || records[0].Rarity != "Rare[R]" {
		t.Fatalf("unexpected record 0 details: %+v", records[0])
	}
	if records[0].Printings[0].Variant != "Normal" {
		t.Fatalf("expected variant Normal, got %s", records[0].Printings[0].Variant)
	}

	// Verify parallel / SPR card
	if records[1].SourceCardID != "BT1-031_SPR" || records[1].Name != "God Break Son Goku" {
		t.Fatalf("unexpected record 1: %+v", records[1])
	}
	if records[1].CollectorNumber != "BT1-031" {
		t.Fatalf("expected base collector number BT1-031, got %s", records[1].CollectorNumber)
	}
	if records[1].Printings[0].Variant != "Parallel" {
		t.Fatalf("expected variant Parallel, got %s", records[1].Printings[0].Variant)
	}
}

func TestDBSFusionWorldOfficialProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		cat := r.URL.Query().Get("category[0]")
		if cat == "583011" {
			_, _ = w.Write([]byte(`
				<ul class="cards">
					<li class="cardItem">
						<a class="cardStr" data-src="detail.php?card_no=FB11-123">
							<img data-src="../../images/cards/FB11-123.webp" alt="FB11-123 Bardock">
						</a>
					</li>
					<li class="cardItem">
						<a class="cardStr" data-src="detail.php?card_no=FB11-123&p=_p2">
							<img data-src="../../images/cards/FB11-123_p2.webp" alt="FB11-123 Bardock">
						</a>
					</li>
				</ul>
			`))
			return
		}
		// Root page with filter list items
		_, _ = w.Write([]byte(`
			<ul class="filterListItems">
				<li><a href="javascript:void(0);" data-val="583011">BOOSTER PACK -BRIGHTNESS OF HOPE- [FB11]</a></li>
			</ul>
		`))
	}))
	defer server.Close()

	provider := &DBSFusionWorldOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL}
	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("unexpected result: count=%d records=%d", result.Count, len(records))
	}

	if records[0].SourceCardID != "FB11-123" || records[0].Name != "Bardock" || records[0].SetCode != "FB11" {
		t.Fatalf("unexpected record 0: %+v", records[0])
	}
	if records[0].Printings[0].Variant != "Normal" {
		t.Fatalf("expected Normal variant, got %s", records[0].Printings[0].Variant)
	}

	if records[1].SourceCardID != "FB11-123_p2" || records[1].CollectorNumber != "FB11-123" {
		t.Fatalf("unexpected record 1: %+v", records[1])
	}
	if records[1].Printings[0].Variant != "Parallel" {
		t.Fatalf("expected Parallel variant, got %s", records[1].Printings[0].Variant)
	}
}

func TestUnionArenaOfficialProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		series := r.URL.Query().Get("series")
		if series == "591117" {
			_, _ = w.Write([]byte(`
				<ul class="cardlistCol">
					<li class="cardImgCol">
						<a class="modalCardDataOpen" href="./detail_iframe.php?card_no=UE17BT/SLG-1-001">
							<img data-src="/na/images/card/UE17BT_SLG-1-001.png?v3" alt="UE17BT/SLG-1-001 Han Song-yi">
						</a>
					</li>
					<li class="cardImgCol">
						<a class="modalCardDataOpen" href="./detail_iframe.php?card_no=UE17BT/SLG-1-003_p1">
							<img data-src="/na/images/card/UE17BT_SLG-1-003_p1.png?v3" alt="UE17BT/SLG-1-003 Han Song-yi">
						</a>
					</li>
				</ul>
			`))
			return
		}
		// Root page with series options
		_, _ = w.Write([]byte(`
			<select name="series">
				<option value="">Select Product</option>
				<option value="591117">SOLO LEVELING [UE17BT]</option>
			</select>
		`))
	}))
	defer server.Close()

	provider := &UnionArenaOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL}
	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("unexpected result: count=%d records=%d", result.Count, len(records))
	}

	if records[0].SourceCardID != "UE17BT/SLG-1-001" || records[0].Name != "Han Song-yi" || records[0].SetCode != "UE17BT" {
		t.Fatalf("unexpected record 0: %+v", records[0])
	}
	if records[0].Printings[0].Variant != "Normal" {
		t.Fatalf("expected Normal variant, got %s", records[0].Printings[0].Variant)
	}

	if records[1].SourceCardID != "UE17BT/SLG-1-003_p1" || records[1].CollectorNumber != "UE17BT/SLG-1-003" {
		t.Fatalf("unexpected record 1: %+v", records[1])
	}
	if records[1].Printings[0].Variant != "Parallel" {
		t.Fatalf("expected Parallel variant, got %s", records[1].Printings[0].Variant)
	}
}

func TestGundamOfficialProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		pkg := r.URL.Query().Get("package")
		if pkg == "616101" {
			_, _ = w.Write([]byte(`
				<div class="resultTxt"><span class="num">2</span>cards found.</div>
				<ul class="cardList">
					<li class="cardItem">
						<a class="cardStr" data-src="detail.php?detailSearch=GD01-001">
							<img data-src="../images/cards/card/GD01-001.webp?261016" alt="Gundam">
						</a>
					</li>
					<li class="cardItem">
						<a class="cardStr" data-src="detail.php?detailSearch=GD01-001_p1">
							<img data-src="../images/cards/card/GD01-001_p1.webp?261016" alt="Gundam">
						</a>
					</li>
				</ul>
			`))
			return
		}
		// Root page with package buttons
		_, _ = w.Write([]byte(`
			<div class="filterListItems">
				<a href="javascript:void(0);" class="js-selectBtn-package" data-val="616101">Newtype Rising [GD01]</a>
			</div>
		`))
	}))
	defer server.Close()

	provider := &GundamOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL}
	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("unexpected result: count=%d records=%d", result.Count, len(records))
	}

	if records[0].SourceCardID != "GD01-001" || records[0].Name != "Gundam" || records[0].SetCode != "GD01" {
		t.Fatalf("unexpected record 0: %+v", records[0])
	}
	if records[0].Printings[0].Variant != "Normal" {
		t.Fatalf("expected Normal variant, got %s", records[0].Printings[0].Variant)
	}

	if records[1].SourceCardID != "GD01-001_p1" || records[1].CollectorNumber != "GD01-001" {
		t.Fatalf("unexpected record 1: %+v", records[1])
	}
	if records[1].Printings[0].Variant != "Parallel" {
		t.Fatalf("expected Parallel variant, got %s", records[1].Printings[0].Variant)
	}
}

func TestBattleSpiritsSagaOfficialProvider(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.Contains(r.URL.Path, "detail.php") {
			cardNo := r.URL.Query().Get("card_no")
			if cardNo == "BSS06-001" {
				_, _ = w.Write([]byte(`
					<dl class="modalInfoCol">
						<dt>
							<div class="infoCol"><span>BSS06-001</span> | <span>X</span></div>
							<div class="cardName">Absolute Divinity Amaterasu Dragon</div>
						</dt>
					</dl>
				`))
				return
			}
			_, _ = w.Write([]byte(`
				<dl class="modalInfoCol">
					<dt>
						<div class="infoCol"><span>BSS06-001</span> | <span>X</span></div>
						<div class="cardName">Absolute Divinity Amaterasu Dragon</div>
					</dt>
				</dl>
			`))
			return
		}

		cat := r.URL.Query().Get("category")
		if cat == "575006" {
			_, _ = w.Write([]byte(`
				<ul class="cardImgList">
					<li class="cardImg">
						<a class="modalOpen" data-src="detail.php?card_no=BSS06-001" href="javascript:;">
							<img src="../images/cards/card/BSS06-001.png" alt="CARD">
						</a>
					</li>
					<li class="cardImg">
						<a class="modalOpen" data-src="detail.php?card_no=BSS06-001_p4" href="javascript:;">
							<img src="../images/cards/card/BSS06-001_p4.png" alt="CARD">
						</a>
					</li>
				</ul>
			`))
			return
		}

		// Root page with category select
		_, _ = w.Write([]byte(`
			<select name="category">
				<option value="">Select series</option>
				<option value="575006">[BSS06] GENERATIONAL LINK</option>
			</select>
		`))
	}))
	defer server.Close()

	provider := &BattleSpiritsSagaOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL}
	records, result := fetchRecords(t, provider)
	if result.Count != 2 || len(records) != 2 {
		t.Fatalf("unexpected result: count=%d records=%d", result.Count, len(records))
	}

	if records[0].SourceCardID != "BSS06-001" || records[0].Name != "Absolute Divinity Amaterasu Dragon" {
		t.Fatalf("unexpected record 0: %+v", records[0])
	}
	if records[0].SetCode != "BSS06" || records[0].Rarity != "X" {
		t.Fatalf("unexpected record 0 details: %+v", records[0])
	}
	if records[0].Printings[0].Variant != "Normal" {
		t.Fatalf("expected Normal variant, got %s", records[0].Printings[0].Variant)
	}

	if records[1].SourceCardID != "BSS06-001_p4" || records[1].CollectorNumber != "BSS06-001" {
		t.Fatalf("unexpected record 1: %+v", records[1])
	}
	if records[1].Printings[0].Variant != "Parallel" {
		t.Fatalf("expected Parallel variant, got %s", records[1].Printings[0].Variant)
	}
}
