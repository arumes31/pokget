import os
import requests
import json
import time
import base64

API_URL = "http://localhost:8080/api/scan"
METADATA_PATH = "test_cards/test_metadata.json"

def run_test():
    if not os.path.exists(METADATA_PATH):
        print("Metadata not found yet. Waiting for download...")
        return

    with open(METADATA_PATH, "r", encoding="utf-8") as f:
        metadata = json.load(f)

    results = []
    
    # We'll test files that exist
    for path, info in metadata.items():
        if not os.path.exists(path):
            continue
            
        print(f"Testing {path} ({info['name']})...")
        try:
            with open(path, "rb") as f:
                files = {"card_image": f}
                data = {"lang": info["lang"]}
                
                start_time = time.time()
                res = requests.post(API_URL, files=files, data=data, timeout=30)
                duration = time.time() - start_time
                
                if res.status_code == 200:
                    result = res.json()
                    detected = result.get("detected", "Unknown Card")
                    # Simple check: is the expected name in the detected name or OCR text?
                    match = info["name"].lower() in detected.lower() or info["name"].lower() in result.get("text", "").lower()
                    
                    results.append({
                        "path": path,
                        "expected": info["name"],
                        "detected": detected,
                        "match": match,
                        "duration": duration,
                        "text": result.get("text", "")
                    })
                    
                    status = "✅" if match else "❌"
                    print(f"{status} {info['name']} -> {detected} ({duration:.2f}s)")
                else:
                    print(f"Error: {res.status_code} - {res.text}")
        except Exception as e:
            print(f"Failed to test {path}: {e}")
            
    # Save results
    with open("test_results.json", "w", encoding="utf-8") as f:
        json.dump(results, f, indent=2)
        
    # Summary
    if results:
        matches = sum(1 for r in results if r["match"])
        total = len(results)
        print(f"\n--- SUMMARY ---")
        print(f"Accuracy: {matches}/{total} ({matches/total*100:.1f}%)")
    else:
        print("No results.")

if __name__ == "__main__":
    run_test()
