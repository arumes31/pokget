import os
import requests
from duckduckgo_search import DDGS
from time import sleep

languages = {
    "eng": "English",
    "jpn": "Japanese",
    "deu": "German",
    "fra": "French",
    "chi_sim": "Simplified Chinese",
    "chi_tra": "Traditional Chinese",
    "kor": "Korean"
}

output_dir = "test_cards"
os.makedirs(output_dir, exist_ok=True)

with DDGS() as ddgs:
    for lang_code, lang_name in languages.items():
        print(f"Searching for {lang_name} cards...")
        query = f"pokemon tcg card scan {lang_name} high resolution"
        
        # We need 5 images
        results = list(ddgs.images(query, max_results=10))
        
        downloaded = 0
        for i, res in enumerate(results):
            if downloaded >= 5:
                break
                
            img_url = res.get('image')
            if not img_url:
                continue
                
            print(f"Downloading {img_url}...")
            try:
                response = requests.get(img_url, timeout=5)
                response.raise_for_status()
                
                # Check if it's actually an image
                content_type = response.headers.get('content-type', '')
                if not content_type.startswith('image/'):
                    continue
                    
                ext = ".jpg"
                if "png" in content_type:
                    ext = ".png"
                elif "webp" in content_type:
                    ext = ".webp"
                    
                file_path = os.path.join(output_dir, f"{lang_code}_{downloaded+1}{ext}")
                with open(file_path, 'wb') as f:
                    f.write(response.content)
                print(f"Saved to {file_path}")
                downloaded += 1
            except Exception as e:
                print(f"Failed: {e}")
            
            sleep(1)
        
        print(f"Finished {lang_name}. Downloaded {downloaded} images.\n")
