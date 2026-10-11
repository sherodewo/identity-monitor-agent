package imagehash

import (
	"bytes"
	"context"
	"github.com/stretchr/testify/require"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"testing"
)

func gradient(t *testing.T, size int, compress bool) []byte {
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			v := uint8(x * 255 / size)
			img.Set(x, y, color.RGBA{v, v, v, 255})
		}
	}
	var b bytes.Buffer
	if compress {
		require.NoError(t, jpeg.Encode(&b, img, &jpeg.Options{Quality: 70}))
	} else {
		require.NoError(t, png.Encode(&b, img))
	}
	return b.Bytes()
}
func TestSimilarity(t *testing.T) {
	a := gradient(t, 128, false)
	b := gradient(t, 64, true)
	m := Matcher{Threshold: 8}
	same, e := m.Compare(context.Background(), ImageInput{a}, ImageInput{a})
	require.NoError(t, e)
	require.True(t, same.Identical)
	different, e := m.Compare(context.Background(), ImageInput{a}, ImageInput{b})
	require.NoError(t, e)
	require.False(t, different.Identical)
	require.True(t, different.Similar)
	_, _, e = Hash([]byte("not an image"))
	require.Error(t, e)
}
