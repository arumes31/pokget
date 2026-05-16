package main

import (
	"fmt"
	"image"
	"image/jpeg"
	_ "image/png"
	"os"

	"github.com/anthonynsimon/bild/adjust"
	"github.com/anthonynsimon/bild/channel"
	"github.com/anthonynsimon/bild/effect"
	"github.com/anthonynsimon/bild/transform"
)

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run generate_crops.go <image_path>")
		return
	}
	imgPath := os.Args[1]

	imgFile, err := os.Open(imgPath)
	if err != nil {
		fmt.Println("Failed to open image:", err)
		return
	}
	defer imgFile.Close()

	src, _, err := image.Decode(imgFile)
	if err != nil {
		fmt.Println("Failed to decode image:", err)
		return
	}

	pipelines := []struct {
		name string
		fn   func(image.Image) image.Image
	}{
		{
			name: "Pipeline 1: Current",
			fn: func(img image.Image) image.Image {
				bounds := img.Bounds()
				res := transform.Resize(img, bounds.Dx()*2, bounds.Dy()*2, transform.Lanczos)
				res = effect.Grayscale(res)
				res = adjust.Contrast(res, 0.3)
				res = adjust.Brightness(res, 0.05)
				res = effect.Sharpen(res)
				return res
			},
		},
		{
			name: "Pipeline 2: Blue Channel Crop",
			fn: func(img image.Image) image.Image {
				bounds := img.Bounds()
				cropRect := image.Rect(int(float64(bounds.Dx())*0.6), int(float64(bounds.Dy())*0.8), bounds.Dx(), bounds.Dy())
				res := transform.Crop(img, cropRect)
				
				res2 := transform.Resize(res, res.Bounds().Dx()*4, res.Bounds().Dy()*4, transform.Lanczos)
				res3 := channel.Extract(res2, channel.Blue)
				res4 := adjust.Contrast(res3, 0.99)
				res5 := adjust.Brightness(res4, 0.2)
				return res5
			},
		},
		{
			name: "Pipeline 3: Full Blue",
			fn: func(img image.Image) image.Image {
				bounds := img.Bounds()
				res := transform.Resize(img, bounds.Dx()*2, bounds.Dy()*2, transform.Lanczos)
				res2 := channel.Extract(res, channel.Blue)
				return res2
			},
		},
		{
			name: "Pipeline 4: Aggressive Sharpen",
			fn: func(img image.Image) image.Image {
				bounds := img.Bounds()
				res := transform.Resize(img, bounds.Dx()*3, bounds.Dy()*3, transform.Lanczos)
				res = effect.Grayscale(res)
				res = effect.Sharpen(res)
				res = adjust.Contrast(res, 0.95)
				return res
			},
		},
	}

	for i, p := range pipelines {
		fmt.Println("Processing:", p.name)
		processed := p.fn(src)
		
		out, err := os.Create(fmt.Sprintf("C:\\Users\\Daniel\\.gemini\\antigravity\\brain\\fedc3a99-0cfa-475a-ad23-c7a9250f467b\\pipeline_%d.jpg", i+1))
		if err != nil {
			fmt.Println("Error creating file:", err)
			continue
		}
		
		err = jpeg.Encode(out, processed, &jpeg.Options{Quality: 90})
		out.Close()
		if err != nil {
			fmt.Println("Error encoding:", err)
		}
	}
}
