package source

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestOnePieceOfficialProviderCleansSeriesDisplayText(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("series") == "569103" {
			_, _ = w.Write([]byte(`<dl class="modalCol" id="ST01-001_p1"><dt><div class="infoCol"><span>ST01-001</span><span>L</span></div><div class="cardName">Luffy</div></dt><dd><div class="frontCol"><img data-src="/images/ST01-001_p1.png"></div></dd></dl>`))
			return
		}
		_, _ = w.Write([]byte(`<select name="series"><option value="ALL">All</option><option value="569103"> Starter Deck&lt;br class="spInline"&gt;Straw Hats &amp;amp; Allies&nbsp; &lt;b&gt;[ST-01]&lt;/b&gt; </option></select>`))
	}))
	defer server.Close()

	records, result := fetchRecords(t, &OnePieceOfficialProvider{HTTP: testHTTP(server), BaseURL: server.URL})
	if result.Count != 1 || len(records) != 1 || len(records[0].Printings) != 1 {
		t.Fatalf("unexpected result=%+v records=%+v", result, records)
	}
	record := records[0]
	want := "Starter Deck Straw Hats & Allies [ST-01]"
	if record.SetName != want || record.Printings[0].SetName != want {
		t.Fatalf("series display names = %q / %q, want %q", record.SetName, record.Printings[0].SetName, want)
	}
	if record.SourceCardID != "ST01-001_p1" || record.Printings[0].SourcePrintingID != "ST01-001_p1" || record.SetCode != "ST-01" || record.Printings[0].SetCode != "ST-01" {
		t.Fatalf("display cleanup changed source identities: %+v", record)
	}
}
