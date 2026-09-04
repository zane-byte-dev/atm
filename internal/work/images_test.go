package work

import (
	"bytes"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"testing"
)

func TestUploadedImageDecodesSupportedFormatsAndRejectsPixelBomb(t *testing.T) {
	picture := image.NewRGBA(image.Rect(0, 0, 2, 2))
	for _, format := range []string{"png", "jpeg", "gif"} {
		var encoded bytes.Buffer
		var err error
		switch format {
		case "png":
			err = png.Encode(&encoded, picture)
		case "jpeg":
			err = jpeg.Encode(&encoded, picture, nil)
		case "gif":
			err = gif.Encode(&encoded, picture, nil)
		}
		if err != nil {
			t.Fatal(err)
		}
		media, _, err := validateUploadedImage(encoded.Bytes())
		if err != nil || media != "image/"+format {
			t.Fatalf("%s: %s %v", format, media, err)
		}
	}
	var encoded bytes.Buffer
	if err := png.Encode(&encoded, picture); err != nil {
		t.Fatal(err)
	}
	// Preserve a valid IHDR CRC so this specifically exercises the dimension
	// check before the expensive pixel decode.
	bomb := append([]byte(nil), encoded.Bytes()...)
	binary.BigEndian.PutUint32(bomb[16:20], 16384)
	binary.BigEndian.PutUint32(bomb[20:24], 16384)
	binary.BigEndian.PutUint32(bomb[29:33], crc32.ChecksumIEEE(bomb[12:29]))
	if _, _, err := validateUploadedImage(bomb); err == nil {
		t.Fatal("accepted a decompression pixel bomb")
	}
}

func TestUploadedGIFBoundsAllFramesBeforeDecoding(t *testing.T) {
	for _, frames := range []int{2, 65} {
		animation := &gif.GIF{}
		for range frames {
			animation.Image = append(animation.Image, image.NewPaletted(image.Rect(0, 0, 2, 2), color.Palette{color.Black, color.White}))
			animation.Delay = append(animation.Delay, 1)
		}
		var data bytes.Buffer
		if err := gif.EncodeAll(&data, animation); err != nil {
			t.Fatal(err)
		}
		_, _, err := validateUploadedImage(data.Bytes())
		if frames == 2 && err != nil {
			t.Fatalf("bounded animation: %v", err)
		}
		if frames == 65 && err == nil {
			t.Fatal("accepted animation beyond decoded frame budget")
		}
	}
}
