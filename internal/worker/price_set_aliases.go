package worker

import "strings"

// priceSetAlias maps one catalog set onto the spelling the marketplace uses.
//
// Some games ship set codes that the marketplace never uses: the UniVersus
// catalog names its sets "TMNT01" or "PLATINUM" while TCGplayer groups them as
// "TMNT" or "CPS", and a few groups have no abbreviation at all. Heuristics cannot
// bridge that, so the mapping is data. `go run ./cmd/pricegaps -unmatched` lists
// the sets that still have no price, which is the input for new entries here.
type priceSetAlias struct {
	// SetCode is the marketplace abbreviation used for the set-code index.
	SetCode string
	// SetName is the marketplace group name used for the set-name index.
	SetName string
}

// priceSetAliases is keyed by "<game>|<catalog set code>". Every entry was checked
// against the TCGCSV group list of its category, so a wrong entry would have to be
// wrong in both the abbreviation and the name.
var priceSetAliases = map[string]priceSetAlias{
	"universus|PLATINUM":        {SetCode: "CPS", SetName: "Capcom Platinum Series"},
	"universus|CAPCOMLEAGUEKIT": {SetCode: "UFSCLK", SetName: "Capcom Casual League Kit"},
	"universus|MHA01":           {SetCode: "MHA", SetName: "UniVersus: My Hero Academia"},
	"universus|YYHDT":           {SetCode: "YDT", SetName: "Yu Yu Hakusho: Dark Tournament"},
	"universus|TMNT01":          {SetCode: "TMNT", SetName: "Teenage Mutant Ninja Turtles"},
	"universus|TK802":           {SetCode: "TK8", SetName: "TEKKEN 8"},
	"universus|FT01":            {SetName: "Fairy Tail: All Fired Up!"},
}

// setAliasKeys returns the match keys a marketplace row would use for this catalog
// set. It is empty when no alias is configured.
func setAliasKeys(game, setCode, number string) (codeKeys, nameKeys []string) {
	alias, ok := priceSetAliases[aliasLookupKey(game, setCode)]
	if !ok {
		return nil, nil
	}
	if alias.SetCode != "" {
		codeKeys = priceMatchKeys(alias.SetCode, number)
	}
	if alias.SetName != "" {
		nameKeys = priceMatchKeys(alias.SetName, number)
	}
	return codeKeys, nameKeys
}

// aliasLookupKey normalizes the game and the catalog set code so a mapping does not
// depend on the marketplace's spelling of either.
func aliasLookupKey(game, setCode string) string {
	return strings.ToLower(strings.TrimSpace(game)) + "|" + strings.ToUpper(strings.TrimSpace(setCode))
}

// knownSetAliases reports how many marketability mappings exist for a game. The
// coverage report prints it so an empty table is visible instead of silent.
func knownSetAliases(game string) int {
	prefix := strings.ToLower(strings.TrimSpace(game)) + "|"
	count := 0
	for key := range priceSetAliases {
		if strings.HasPrefix(key, prefix) {
			count++
		}
	}
	return count
}
