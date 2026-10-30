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
	}
}
