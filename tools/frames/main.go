// Extract each frame of a GIF into individual PNGs and emit a manifest with
// the GIF's original per-frame delays (in ms) as a starting point for tuning.
//
// Usage: go run ./tools/frames -out assets/sprites/frames/dive assets/sprites/dive.gif
package main

import (
	"encoding/json"
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/gif"
	"image/png"
	"log"
	"os"
	"path/filepath"
)

type frameMeta struct {
	File string `json:"file"`
	Ms   int    `json:"ms"`
}

type manifest struct {
	Width  int         `json:"width"`
	Height int         `json:"height"`
	Frames []frameMeta `json:"frames"`
}

func main() {
	outDir := flag.String("out", "", "output directory")
	flag.Parse()
	if flag.NArg() < 1 || *outDir == "" {
		log.Fatal("usage: frames -out <dir> <input.gif>")
	}

	f, err := os.Open(flag.Arg(0))
	if err != nil {
		log.Fatal(err)
	}
	defer f.Close()

	g, err := gif.DecodeAll(f)
	if err != nil {
		log.Fatal(err)
	}
	if err := os.MkdirAll(*outDir, 0o755); err != nil {
		log.Fatal(err)
	}

	bounds := image.Rect(0, 0, g.Config.Width, g.Config.Height)
	canvas := image.NewRGBA(bounds)

	m := manifest{Width: g.Config.Width, Height: g.Config.Height}
	for i, frame := range g.Image {
		// Snapshot for DisposalPrevious.
		prev := image.NewRGBA(bounds)
		draw.Draw(prev, bounds, canvas, image.Point{}, draw.Src)

		draw.Draw(canvas, frame.Bounds(), frame, frame.Bounds().Min, draw.Over)

		name := fmt.Sprintf("%03d.png", i)
		out, err := os.Create(filepath.Join(*outDir, name))
		if err != nil {
			log.Fatal(err)
		}
		if err := png.Encode(out, canvas); err != nil {
			log.Fatal(err)
		}
		out.Close()

		m.Frames = append(m.Frames, frameMeta{File: name, Ms: g.Delay[i] * 10})

		switch g.Disposal[i] {
		case gif.DisposalBackground:
			draw.Draw(canvas, frame.Bounds(), image.Transparent, image.Point{}, draw.Src)
		case gif.DisposalPrevious:
			draw.Draw(canvas, bounds, prev, image.Point{}, draw.Src)
		}
	}

	mf, err := os.Create(filepath.Join(*outDir, "frames.json"))
	if err != nil {
		log.Fatal(err)
	}
	defer mf.Close()
	enc := json.NewEncoder(mf)
	enc.SetIndent("", "  ")
	if err := enc.Encode(m); err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Wrote %d frames to %s\n", len(g.Image), *outDir)
}
