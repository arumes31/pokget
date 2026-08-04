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
	}
}
