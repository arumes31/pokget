package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestBenchmarkCasesCoverEveryTCGAndAbstention(t *testing.T) {
	counts := make(map[string]int)
	unknown := 0
	for _, benchmarkCase := range benchmarkCases() {
		counts[benchmarkCase.Game]++
		if benchmarkCase.WantID == "NONE" {
			unknown++
		}
	}
	for _, game := range []string{"pokemon", "magic", "one_piece", "lorcana", "weiss_schwarz", "yugioh"} {
		if counts[game] < 2 {
			t.Fatalf("%s cases = %d, want at least 2", game, counts[game])
		}
	}
	if unknown < 2 {
		t.Fatalf("unknown cases = %d, want at least 2", unknown)
	}
}

func TestRunCaseAcceptsOnlySuppliedID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(request.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["format"] == nil {
			t.Fatal("request omitted structured-output schema")
		}
		_ = json.NewEncoder(writer).Encode(generateResponse{
			Response:     `{"card_id":"pokemon-charizard"}`,
			EvalCount:    4,
			EvalDuration: int64(time.Second),
		})
	}))
	defer server.Close()

	benchmarkCase := benchmarkCases()[0]
	result := runCase(context.Background(), server.Client(), server.URL, "test", benchmarkCase)
	if !result.Valid || !result.Correct {
		t.Fatalf("runCase() = %+v, want a valid exact result", result)
	}
	if result.TokensSecond != 4 {
		t.Fatalf("tokens/s = %v, want 4", result.TokensSecond)
	}
}

func TestSplitModels(t *testing.T) {
	models := splitModels(" tinyllama, ,qwen2.5:0.5b ")
	if len(models) != 2 || models[0] != "tinyllama" || models[1] != "qwen2.5:0.5b" {
		t.Fatalf("splitModels() = %#v", models)
	}
}
