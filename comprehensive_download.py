import os
import requests
import json
import time
from duckduckgo_search import DDGS

OUTPUT_DIR = "test_cards"
os.makedirs(OUTPUT_DIR, exist_ok=True)

LANGS = {
    "eng": {"tcgdex": "en", "scryfall": "en", "ygopro": "english", "ddg": "English"},
    "fra": {"tcgdex": "fr", "scryfall": "fr", "ygopro": "french", "ddg": "French"},
    "deu": {"tcgdex": "de", "scryfall": "de", "ygopro": "german", "ddg": "German"},
    "jpn": {"tcgdex": "ja", "scryfall": "ja", "ygopro": "japanese", "ddg": "Japanese"},
    "kor": {"tcgdex": "ko", "scryfall": "ko", "ygopro": "korean", "ddg": "Korean"},
    "chi_sim": {"tcgdex": "zh-cn", "scryfall": "zhs", "ygopro": "chinese", "ddg": "Simplified Chinese"},
    "chi_tra": {"tcgdex": "zh-tw", "scryfall": "zht", "ygopro": "chinese", "ddg": "Traditional Chinese"}
}

GAMES = ["Pokemon", "Magic", "One Piece", "Lorcana", "Weiss Schwarz", "Yu-Gi-Oh"]

METADATA_PATH = os.path.join(OUTPUT_DIR, "test_metadata.json")
metadata = {}

if os.path.exists(METADATA_PATH):
    try:
        with open(METADATA_PATH, "r", encoding="utf-8") as f:
            metadata = json.load(f)
    except:
        pass

def save_metadata():
    with open(METADATA_PATH, "w", encoding="utf-8") as f:
        json.dump(metadata, f, ensure_ascii=False, indent=2)

def download_image(url, path):
    if os.path.exists(path):
        return True
    try:
        res = requests.get(url, timeout=10)
        res.raise_for_status()
        with open(path, "wb") as f:
            f.write(res.content)
        return True
    except Exception as e:
        print(f"Failed to download {url}: {e}")
        return False

def get_pokemon_cards(lang_code, limit=50):
    tcgdex_lang = LANGS[lang_code]["tcgdex"]
    print(f"Fetching Pokemon cards for {lang_code}...")
    url = f"https://api.tcgdex.net/v2/{tcgdex_lang}/cards"
    try:
        res = requests.get(url, timeout=10)
        res.raise_for_status()
        cards = [c for c in res.json() if "image" in c]
        return cards[:limit]
    except Exception as e:
        print(f"Error fetching Pokemon cards: {e}")
        return []

def get_mtg_cards(lang_code, limit=50):
    scryfall_lang = LANGS[lang_code]["scryfall"]
    print(f"Fetching MTG cards for {lang_code}...")
    url = f"https://api.scryfall.com/cards/search?q=lang:{scryfall_lang}&order=random"
    try:
        res = requests.get(url, timeout=10)
        res.raise_for_status()
        data = res.json()
        cards = data.get("data", [])
        return cards[:limit]
    except Exception as e:
        print(f"Error fetching MTG cards: {e}")
        return []

def get_yugioh_cards(lang_code, limit=50):
    # YGOPRODECK API doesn't have a direct language filter in the search query for all languages,
    # but it provides translations for some. We'll use random cards and try to find translations.
    print(f"Fetching Yu-Gi-Oh cards for {lang_code}...")
    url = "https://db.ygoprodeck.com/api/v7/cardinfo.php"
    try:
        res = requests.get(url, timeout=10)
        res.raise_for_status()
        data = res.json()
        all_cards = data.get("data", [])
        # Just take first N cards for now, as language-specific image URLs are harder to get via API
        # Most images are English. For Japanese/other, we might need DDG.
        if lang_code == "eng":
            return all_cards[:limit]
        else:
            return get_ddg_cards("Yu-Gi-Oh", LANGS[lang_code]["ddg"], limit)
    except Exception as e:
        print(f"Error fetching Yu-Gi-Oh cards: {e}")
        return []

def get_onepiece_cards(lang_code, limit=50):
    # One Piece doesn't have a great public API like Pokemon/MTG yet.
    # We'll use DDG for all languages.
    return get_ddg_cards("One Piece", LANGS[lang_code]["ddg"], limit)

def get_ddg_cards(game, lang_name, limit=50):
    print(f"Searching DuckDuckGo for {game} cards in {lang_name}...")
    query = f"{game} tcg card scan {lang_name} high resolution"
    downloaded = []
    try:
        with DDGS() as ddgs:
            results = list(ddgs.images(query, max_results=limit*2))
            for res in results:
                if len(downloaded) >= limit:
                    break
                img_url = res.get('image')
                if img_url:
                    downloaded.append({"name": f"{game} Card", "image": img_url})
    except Exception as e:
        print(f"Error searching DDG for {game} in {lang_name}: {e}")
    return downloaded

for game in GAMES:
    game_dir = os.path.join(OUTPUT_DIR, game.replace(" ", "_"))
    os.makedirs(game_dir, exist_ok=True)
    
    for lang_code in LANGS:
        lang_dir = os.path.join(game_dir, lang_code)
        os.makedirs(lang_dir, exist_ok=True)
        
        cards = []
        if game == "Pokemon":
            cards = get_pokemon_cards(lang_code)
        elif game == "Magic":
            cards = get_mtg_cards(lang_code)
        elif game == "Yu-Gi-Oh":
            cards = get_yugioh_cards(lang_code)
        elif game == "One Piece":
            cards = get_onepiece_cards(lang_code)
        else:
            cards = get_ddg_cards(game, LANGS[lang_code]["ddg"])
            
        print(f"Downloading {len(cards)} cards for {game} ({lang_code})...")
        for i, card in enumerate(cards):
            img_url = ""
            name = ""
            
            if game == "Pokemon":
                img_url = f"{card['image']}/high.webp"
                name = card["name"]
            elif game == "Magic":
                img_url = card.get("image_uris", {}).get("normal", "")
                name = card.get("name", "Unknown MTG Card")
            elif game == "Yu-Gi-Oh" and lang_code == "eng" and "card_images" in card:
                img_url = card["card_images"][0]["image_url"]
                name = card["name"]
            else:
                # From DDG or fallback
                img_url = card.get("image")
                name = card.get("name", f"{game} Card {i+1}")
                
            if not img_url:
                continue
                
            ext = ".webp"
            if "png" in img_url: ext = ".png"
            elif "jpg" in img_url or "jpeg" in img_url: ext = ".jpg"
            
            filename = f"{i+1}{ext}"
            path = os.path.join(lang_dir, filename)
            
            # Normalize path for JSON/metadata
            json_path = path.replace("\\", "/")

            if download_image(img_url, path):
                metadata[json_path] = {
                    "game": game,
                    "lang": lang_code,
                    "name": name
                }
                save_metadata()
            
            time.sleep(0.1)

print("Done downloading cards.")
