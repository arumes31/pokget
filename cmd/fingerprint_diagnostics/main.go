package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"os"
	"path/filepath"

	"pokget/internal/db"
	"pokget/internal/detectiontest"
	"pokget/internal/service"

	_ "golang.org/x/image/webp"
)

type candidate struct {
	ID       string `json:"id"`
	Name     string `json:"name"`
	Distance int    `json:"distance"`
}

type diagnostic struct {
	Game       string      `json:"game"`
	Variant    string      `json:"variant"`
	Expected   string      `json:"expected"`
	Candidates []candidate `json:"candidates"`
}

func main() {
	fixtureDir := flag.String("fixtures", "artifacts/detection/v1-seed-20260804-count-6", "fixture run")
	variant := flag.String("variant", "", "only inspect one variant")
	flag.Parse()
	if err := run(*fixtureDir, *variant); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(fixtureDir, selectedVariant string) error {
	payload, err := os.ReadFile(filepath.Join(fixtureDir, "selection.json"))
	if err != nil {
		return err
	}
	var manifest detectiontest.OutputManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return err
	}
	if err := detectiontest.VerifyRun(fixtureDir, manifest.Version, detectiontest.ManifestSHA256(), manifest.Seed, manifest.SelectionCount); err != nil {
		return err
	}
	database, err := db.Connect()
	if err != nil {
		return err
	}
	defer database.Close()
	fingerprints := service.NewFingerprintService(database)
	encoder := json.NewEncoder(os.Stdout)
	encoder.SetIndent("", "  ")
	for _, selected := range manifest.Cards {
		for _, artifact := range selected.Artifacts {
			if selectedVariant != "" && artifact.Variant != selectedVariant {
				continue
			}
			file, err := os.Open(filepath.Join(fixtureDir, filepath.FromSlash(artifact.Path)))
			if err != nil {
				return err
			}
			decoded, _, decodeErr := image.Decode(file)
			closeErr := file.Close()
			if decodeErr != nil {
				return decodeErr
			}
			if closeErr != nil {
				return closeErr
			}
			hash, err := fingerprints.CalculateHash(decoded)
			if err != nil {
				return err
			}
			match := fingerprints.SearchByHash(hash)
			row := diagnostic{Game: selected.Card.Game, Variant: artifact.Variant, Expected: selected.Card.Name}
			if match != nil {
				limit := min(10, len(match.Potential))
				for _, potential := range match.Potential[:limit] {
					row.Candidates = append(row.Candidates, candidate{
						ID: potential.Card.ID, Name: potential.Card.Name, Distance: potential.Distance,
					})
				}
			}
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
	}
	return nil
}
