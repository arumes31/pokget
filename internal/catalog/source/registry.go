package source

import "pokget/internal/catalog"

func DefaultProviders(options HTTPOptions, language string, maxWeissPages int) []catalog.Provider {
	return []catalog.Provider{
		&TCGdexProvider{HTTP: options, Language: language},
		&ScryfallProvider{HTTP: options},
		&OnePieceOfficialProvider{HTTP: options},
		&LorcanaJSONProvider{HTTP: options, Language: language},
		&WeissProvider{HTTP: options, MaxPages: maxWeissPages},
		&YGOPRODeckProvider{HTTP: options},
		&SWUDBProvider{HTTP: options},
		&FabCubeProvider{HTTP: options},
		&DigimonCardIOProvider{HTTP: options},
		&GATCGProvider{HTTP: options},
		&AlteredProvider{HTTP: options, Language: language},
		&SorceryProvider{HTTP: options},
		&DBSMastersOfficialProvider{HTTP: options},
		&DBSFusionWorldOfficialProvider{HTTP: options},
		&UnionArenaOfficialProvider{HTTP: options},
		&GundamOfficialProvider{HTTP: options},
		&BattleSpiritsSagaOfficialProvider{HTTP: options},
		&WoWTCGProvider{HTTP: options},
		&FFTCGProvider{HTTP: options, Language: language},
		&ShadowverseEvolveProvider{HTTP: options},
		&GodzillaProvider{HTTP: options},
		&UniVersusProvider{HTTP: options},
		&VanguardProvider{HTTP: options},
		&HololiveProvider{HTTP: options},
		&DuelMastersProvider{HTTP: options},
		&TCGCSVProvider{HTTP: options, CategoryID: 90, GameType: catalog.GameCookieRun, ProviderID: "cookierun_braverse"},
		&TCGCSVProvider{HTTP: options, CategoryID: 17, GameType: catalog.GameForceOfWill, ProviderID: "force_of_will"},
		&TCGCSVProvider{HTTP: options, CategoryID: 67, GameType: catalog.GameWIXOSS, ProviderID: "wixoss"},
		&TCGCSVProvider{HTTP: options, CategoryID: 78, GameType: catalog.GameAlphaClash, ProviderID: "alpha_clash"},
		&TCGCSVProvider{HTTP: options, CategoryID: 89, GameType: catalog.GameRiftbound, ProviderID: "riftbound"},
	}
}
