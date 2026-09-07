from pathlib import Path
from PIL import Image
source = Path(r'C:\Users\Daniel\.codex\generated_images\01a07d75-f4b8-7e80-89d9-295f9ec1ff7d')
target = Path('static/img/auth')
target.mkdir(parents=True, exist_ok=True)
for name, filename in [('celestial', 'exec-084bd041-7c9d-4b56-809d-4641a8684d16.png'), ('botanical', 'exec-5d5e5890-0156-4e62-ab74-ce7a6e921f39.png'), ('mythic', 'exec-55b6159a-ed0f-4a61-8b88-dcb10515d817.png')]:
    with Image.open(source / filename) as image:
        image.save(target / f'{name}.webp', 'WEBP', quality=82, method=6)
        print(name, image.size, (target / f'{name}.webp').stat().st_size)
