package imagehash

import (
	"bytes"
	"context"
	"crypto/sha256"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math/bits"
)

type ImageInput struct{ Data []byte }
type MatchResult struct {
	Identical      bool   `json:"identical"`
	Similar        bool   `json:"similar"`
	Distance       int    `json:"distance"`
	SHA256         string `json:"sha256"`
	PerceptualHash string `json:"perceptual_hash"`
}
type ImageMatcher interface {
	Compare(context.Context, ImageInput, ImageInput) (MatchResult, error)
}
type Matcher struct{ Threshold int }

func Hash(data []byte) (string, uint64, error) {
	if len(data) > 10<<20 {
		return "", 0, fmt.Errorf("image exceeds 10 MB")
	}
	cfg, _, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		return "", 0, e
	}
	if cfg.Width <= 0 || cfg.Height <= 0 || int64(cfg.Width)*int64(cfg.Height) > 25000000 {
		return "", 0, fmt.Errorf("image dimensions exceed limit")
	}
	img, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return "", 0, e
	}
	bounds := img.Bounds()
	var grid [72]uint64
	var counts [72]uint64
	// Average each cell before dHash to make resize/compression comparison stable.
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		gy := (y - bounds.Min.Y) * 8 / bounds.Dy()
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			gx := (x - bounds.Min.X) * 9 / bounds.Dx()
			r, g, b, _ := img.At(x, y).RGBA()
			i := gy*9 + gx
			grid[i] += uint64(r)*299 + uint64(g)*587 + uint64(b)*114
			counts[i]++
		}
	}
	for i := range grid {
		if counts[i] > 0 {
			grid[i] /= counts[i]
		}
	}
	var hash uint64
	for y := 0; y < 8; y++ {
		for x := 0; x < 8; x++ {
			if grid[y*9+x] > grid[y*9+x+1] {
				hash |= 1 << uint(y*8+x)
			}
		}
	}
	return fmt.Sprintf("%x", sha256.Sum256(data)), hash, nil
}
func (m Matcher) Compare(ctx context.Context, reference, candidate ImageInput) (MatchResult, error) {
	if e := ctx.Err(); e != nil {
		return MatchResult{}, e
	}
	sha, a, e := Hash(reference.Data)
	if e != nil {
		return MatchResult{}, e
	}
	other, b, e := Hash(candidate.Data)
	if e != nil {
		return MatchResult{}, e
	}
	d := bits.OnesCount64(a ^ b)
	return MatchResult{Identical: sha == other, Similar: d <= m.Threshold, Distance: d, SHA256: other, PerceptualHash: fmt.Sprintf("%016x", b)}, nil
}
