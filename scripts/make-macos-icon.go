package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/color"
	"image/png"
	"os"
)

func main() {
	if len(os.Args) != 3 {
		panic("usage: make-macos-icon output.icns preview.png")
	}
	canvas := image.NewRGBA(image.Rect(0, 0, 1024, 1024))
	fillRound(canvas, image.Rect(58, 58, 966, 966), 210, color.RGBA{R: 22, G: 107, B: 83, A: 255})
	fillRound(canvas, image.Rect(320, 174, 704, 514), 38, color.RGBA{R: 250, G: 253, B: 251, A: 255})
	fillRound(canvas, image.Rect(190, 390, 834, 724), 70, color.RGBA{R: 234, G: 245, B: 239, A: 255})
	fillRound(canvas, image.Rect(263, 460, 761, 645), 35, color.RGBA{R: 18, G: 72, B: 57, A: 255})
	fillRound(canvas, image.Rect(330, 600, 694, 850), 27, color.RGBA{R: 255, G: 255, B: 255, A: 255})
	fillRound(canvas, image.Rect(374, 234, 650, 268), 12, color.RGBA{R: 53, G: 133, B: 100, A: 255})
	fillRound(canvas, image.Rect(374, 294, 650, 322), 10, color.RGBA{R: 196, G: 222, B: 207, A: 255})
	fillRound(canvas, image.Rect(374, 345, 597, 373), 10, color.RGBA{R: 196, G: 222, B: 207, A: 255})
	fillRound(canvas, image.Rect(374, 674, 650, 701), 9, color.RGBA{R: 196, G: 222, B: 207, A: 255})
	fillRound(canvas, image.Rect(374, 728, 610, 755), 9, color.RGBA{R: 196, G: 222, B: 207, A: 255})
	fillCircle(canvas, 714, 506, 19, color.RGBA{R: 132, G: 221, B: 169, A: 255})

	var icns bytes.Buffer
	icns.WriteString("icns")
	_ = binary.Write(&icns, binary.BigEndian, uint32(0))
	for _, part := range []struct {
		name string
		size int
	}{
		{"icp4", 16}, {"icp5", 32}, {"icp6", 64}, {"ic07", 128},
		{"ic08", 256}, {"ic09", 512}, {"ic10", 1024},
	} {
		pixels := scaleNearest(canvas, part.size)
		var encoded bytes.Buffer
		if err := png.Encode(&encoded, pixels); err != nil {
			panic(err)
		}
		icns.WriteString(part.name)
		_ = binary.Write(&icns, binary.BigEndian, uint32(encoded.Len()+8))
		_, _ = icns.Write(encoded.Bytes())
	}
	binary.BigEndian.PutUint32(icns.Bytes()[4:8], uint32(icns.Len()))
	if err := os.WriteFile(os.Args[1], icns.Bytes(), 0644); err != nil {
		panic(err)
	}
	file, err := os.Create(os.Args[2])
	if err != nil {
		panic(err)
	}
	if err := png.Encode(file, canvas); err != nil {
		_ = file.Close()
		panic(err)
	}
	if err := file.Close(); err != nil {
		panic(err)
	}
}

func scaleNearest(source *image.RGBA, size int) *image.RGBA {
	result := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			result.Set(x, y, source.At(x*source.Bounds().Dx()/size, y*source.Bounds().Dy()/size))
		}
	}
	return result
}

func fillRound(dst *image.RGBA, rect image.Rectangle, radius int, value color.Color) {
	bounds := dst.Bounds().Intersect(rect)
	r2 := radius * radius
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			cx, cy := x, y
			if x < rect.Min.X+radius {
				cx = rect.Min.X + radius
			} else if x >= rect.Max.X-radius {
				cx = rect.Max.X - radius - 1
			}
			if y < rect.Min.Y+radius {
				cy = rect.Min.Y + radius
			} else if y >= rect.Max.Y-radius {
				cy = rect.Max.Y - radius - 1
			}
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				dst.Set(x, y, value)
			}
		}
	}
}

func fillCircle(dst *image.RGBA, cx, cy, radius int, value color.Color) {
	bounds := image.Rect(cx-radius, cy-radius, cx+radius, cy+radius).Intersect(dst.Bounds())
	r2 := radius * radius
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			dx, dy := x-cx, y-cy
			if dx*dx+dy*dy <= r2 {
				dst.Set(x, y, value)
			}
		}
	}
}
