package source

import "testing"

func TestDefaultImageHostsCoversPrimaryProviders(t *testing.T) {
	t.Parallel()

	hosts := DefaultImageHosts()
	for _, provider := range DefaultProviders(HTTPOptions{}, "en", 1) {
		configured := hosts[provider.ID()]
		if len(configured) == 0 {
			t.Fatalf("primary provider %q has no image hostname allowlist", provider.ID())
		}
		for _, host := range configured {
			if host == "" {
				t.Fatalf("primary provider %q has an empty allowed hostname", provider.ID())
			}
		}
	}
}
