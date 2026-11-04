package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"image"
	"io"
	"net/http"
	"os"
	"time"

	"pokget/internal/db"
	"pokget/internal/service"
)

func main() {
	game := flag.String("game", "pokemon", "game")
	language := flag.String("language", "en", "language")
	limit := flag.Int("limit", 20, "cards")
	id := flag.String("id", "", "single card id")
	flag.Parse()

	database, err := db.Connect()
	if err != nil {
		panic(err)
	}
	defer database.Close()
	rows, err := database.Query(`SELECT id, coalesce(image_url,''), phash FROM cards
		WHERE game=$1 AND language=$2 AND catalog_active AND superseded_by_card_id IS NULL
		  AND phash IS NOT NULL AND coalesce(image_url,'') <> ''
		  AND ($4 = '' OR id = $4)
		ORDER BY md5(id || '|probe') LIMIT $3`, *game, *language, *limit, *id)
	if err != nil {
		panic(err)
	}
	defer rows.Close()

	fingerprint := service.NewFingerprintService(nil)
	client := &http.Client{Timeout: 30 * time.Second}
	distances := make([]int, 0, *limit)
	for rows.Next() {
		var id, url string
		var stored int64
		if err := rows.Scan(&id, &url, &stored); err != nil {
			panic(err)
		}
		request, err := http.NewRequestWithContext(context.Background(), http.MethodGet, url, nil)
		if err != nil {
			continue
		}
		request.Header.Set("User-Agent", "pokget-phash-probe/1.0")
		response, err := client.Do(request)
		if err != nil {
			continue
		}
		payload, _ := io.ReadAll(io.LimitReader(response.Body, 16<<20))
		_ = response.Body.Close()
		if response.StatusCode != http.StatusOK {
			continue
		}
		decoded, _, err := image.Decode(bytes.NewReader(payload))
		if err != nil {
			continue
		}
		hash, err := fingerprint.CalculateHash(decoded)
		if err != nil {
			continue
		}
		distance := service.HashDistance(hash, stored)
		distances = append(distances, distance)
		fmt.Printf("%s\t%d\n", id, distance)
	}
	fmt.Fprintln(os.Stderr, "distances:", distances)
}
