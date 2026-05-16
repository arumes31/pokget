import os
import requests
import json
import time

output_dir = "test_cards"
os.makedirs(output_dir, exist_ok=True)

langs = {
    "eng": "en",
    "fra": "fr",
    "deu": "de",
    "jpn": "ja",
    "kor": "ko",
    "chi_sim": "zh-cn",
    "chi_tra": "zh-tw"
}

metadata_path = os.path.join(output_dir, "test_cards_metadata.json")
metadata = {}

if os.path.exists(metadata_path):
    with open(metadata_path, "r", encoding="utf-8") as f:
        metadata = json.load(f)

def save_metadata():
    with open(metadata_path, "w", encoding="utf-8") as f:
        json.dump(metadata, f, ensure_ascii=False, indent=2)

for prefix, tcgdex_lang in langs.items():
    print(f"Fetching card list for {prefix} ({tcgdex_lang})...")
    url = f"https://api.tcgdex.net/v2/{tcgdex_lang}/cards"
    try:
        res = requests.get(url, timeout=10)
        res.raise_for_status()
        cards = res.json()
    except Exception as e:
        print(f"Failed to fetch card list for {tcgdex_lang}: {e}")
        continue

    # Filter cards that have an image field
    cards_with_images = [c for c in cards if "image" in c]
    print(f"Found {len(cards_with_images)} cards with images for {prefix}.")

    # Take first 100
    to_download = cards_with_images[:100]
    print(f"Downloading 100 cards for {prefix}...")

    for idx, card in enumerate(to_download, 1):
        card_id = card["id"]
        card_name = card["name"]
        card_number = card["localId"]
        image_base_url = card["image"]
        
        # Construct high res image URL
        image_url = f"{image_base_url}/high.webp"
        
        filename = f"{prefix}_{card_id}.webp"
        path = os.path.join(output_dir, filename)
        
        # Save metadata
        metadata[filename] = {
            "name": card_name,
            "number": card_number,
            "lang": prefix
        }
        
        if os.path.exists(path):
            print(f"[{idx}/100] {filename} already exists, skipping.")
            # Still save metadata in case it was missing
            save_metadata()
            continue
            
        print(f"[{idx}/100] Downloading {image_url} -> {path}")
        try:
            img_res = requests.get(image_url, timeout=10)
            img_res.raise_for_status()
            with open(path, "wb") as f:
                f.write(img_res.content)
            
            # Save metadata after successful download
            save_metadata()
            
            # Small delay to be polite
            time.sleep(0.1)
        except Exception as e:
            print(f"Failed to download {image_url}: {e}")

print("Done downloading cards and saving metadata.")
