package worms

import (
	"bufio"
	"encoding/binary"
	"hash/crc32"
	"image"
	"image/color"
	"io"
)

// grays is a slice of 256 grayscale colors.
var grays = make([]color.Color, 0x100)

// init initializes the grays slice with grayscale colors equal to their index.
func init() {
	for i := range grays {
		grays[i] = color.Gray{Y: uint8(i)}
	}
}

// drawFill fills an image with a specified color index.
func drawFill(im *image.Paletted, ci uint8) {
	if len(im.Pix) > 0 {
		im.Pix[0] = ci
		for i := 1; i < len(im.Pix); i *= 2 {
			copy(im.Pix[i:], im.Pix[:i])
		}
	}
}

// glowPlotter is a custom plotter for rendering glow effects on an image.
type glowPlotter struct{ *image.Paletted }

// Set sets the color at the specified position (x, y) on the image using a color.Color.
func (p *glowPlotter) Set(x, y int, c color.Color) {
	p.SetColorIndex(x, y, c.(color.Gray).Y)
}

// SetColorIndex sets the color index at the specified position (x, y) on the image.
func (p *glowPlotter) SetColorIndex(x, y int, ci uint8) {
	// Adjust the neighboring pixels to create a glow effect
	if p.setPixIfLower(x, y, ci) {
		const sqrt2 = 1.414213562
		if i := float64(ci) * sqrt2; i < float64(len(p.Palette)-2) {
			ci = uint8(i)
			p.setPixIfLower(x-1, y, ci)
			p.setPixIfLower(x, y-1, ci)
			p.setPixIfLower(x+1, y, ci)
			p.setPixIfLower(x, y+1, ci)
		}
		if i := float64(ci) * sqrt2; i < float64(len(p.Palette)-2) {
			ci = uint8(i)
			p.setPixIfLower(x-1, y-1, ci)
			p.setPixIfLower(x-1, y+1, ci)
			p.setPixIfLower(x+1, y-1, ci)
			p.setPixIfLower(x+1, y+1, ci)
		}
	}
}

// setPixIfLower sets the color index of the pixel at (x, y) if the provided color index is lower.
// It returns true if the pixel color was updated.
func (p *glowPlotter) setPixIfLower(x, y int, ci uint8) bool {
	// Check if the pixel is within the image bounds
	if (image.Point{X: x, Y: y}.In(p.Rect)) {
		i := p.PixOffset(x, y)
		// Update the pixel color if the new color index is lower
		if p.Pix[i] > ci {
			p.Pix[i] = ci
			return true
		}
	}
	return false
}

// optimizeFrames optimizes the frames in the given slice of images.
// It attempts to reduce redundancy between consecutive frames by
// compressing repeated regions into transparent pixels or by cropping
// unchanged areas. The function modifies the provided slice in place.
func optimizeFrames(ims []*image.Paletted) {
	if len(ims) == 0 {
		return
	}

	// Create a buffer image to hold the optimized frame
	buf := image.NewPaletted(ims[0].Rect, ims[0].Palette)
	// Initialize a transparent pixel array to replace repeating regions
	trans := []uint8{uint8(len(ims[0].Palette) - 1)}

	// Iterate over each frame to optimize redundancy
	for i, im := range ims {
		if i == 0 {
			// Copy the pixel data from the first frame to the buffer
			copy(buf.Pix, im.Pix)
		} else {
			var same bool
			var j0, x0, y0 int
			var crop image.Rectangle

			// Scan the pixel data to find repeating regions
			for j := 0; j <= len(im.Pix); j++ {
				if j == 0 {
					same = buf.Pix[j] == im.Pix[j]
				} else if j == len(im.Pix) || (buf.Pix[j] == im.Pix[j]) != same {
					x := j % im.Stride
					y := j / im.Stride

					if same {
						// If the region is identical, replace it with transparent pixels
						for len(trans) < j-j0 {
							trans = append(trans, trans...)
						}
						copy(im.Pix[j0:j], trans[:j-j0])
					} else {
						// If the region is different, copy it to the buffer and adjust the crop area
						copy(buf.Pix[j0:j], im.Pix[j0:j])
						var r image.Rectangle
						if y > y0 {
							r = image.Rect(0, y0, im.Stride, y+1)
						} else {
							r = image.Rect(x0, y0, x, y+1)
						}
						if crop.Empty() {
							crop = r
						} else {
							crop = crop.Union(r)
						}
					}
					same = !same
					j0, x0, y0 = j, x, y
				}
			}

			// If no change occurred, set a default crop area
			if crop.Empty() {
				crop = image.Rect(0, 0, 1, 1)
			}

			// Update the frame by cropping it to the adjusted area
			ims[i] = im.SubImage(crop).(*image.Paletted)
		}
	}
}

// gifWriter is a custom writer for writing GIF files with additional comments.
type gifWriter struct {
	*bufio.Writer        // Underlying writer
	Comment       string // Comment to be added to the GIF file
	done          bool   // Flag indicating whether the writing process is complete
}

// Write writes the contents of the byte slice to the writer.
// It intercepts the application extension to insert the comment before writing.
func (w *gifWriter) Write(p []byte) (nn int, err error) {
	n := 0
	if !w.done {
		// Intercept application extension and insert comment
		if len(p) == 3 && p[0] == 0x21 && p[1] == 0xff && p[2] == 0x0b {
			// Write the comment extension
			if n, err = w.writeExtension([]byte(w.Comment), 0xfe); err != nil {
				return
			} else {
				nn += n
			}
			w.done = true
		}
	}
	// Write the content of the byte slice
	if n, err = w.Writer.Write(p); err != nil {
		return
	} else {
		nn += n
	}
	return
}

// writeExtension writes the comment extension into the GIF file.
func (w *gifWriter) writeExtension(b []byte, e byte) (nn int, err error) {
	n := 0
	// Write the extension header
	if n, err = w.Writer.Write([]byte{0x21, e, byte(len(b))}); err != nil {
		return
	} else {
		nn += n
	}
	// Write the comment data
	if n, err = w.Writer.Write(b); err != nil {
		return
	} else {
		nn += n
	}
	// Write the extension terminator
	if err = w.Writer.WriteByte(0); err != nil {
		return
	} else {
		nn++
	}
	return
}

// pngWriter is a custom writer for writing PNG files with additional text metadata.
type pngWriter struct {
	io.Writer        // Underlying writer
	Text      string // Text metadata to be added to the PNG file
	done      bool   // Flag indicating whether the writing process is complete
}

// Write writes the contents of the byte slice to the writer.
func (w *pngWriter) Write(p []byte) (nn int, err error) {
	n := 0
	if !w.done {
		// Intercept the first data chunk and insert text metadata
		if len(p) >= 8 && string(p[4:8]) == "IDAT" {
			// Write the text metadata chunk
			if n, err = w.writeChunk([]byte(w.Text), "tEXt"); err != nil {
				return
			} else {
				nn += n
			}
			w.done = true
		}
	}
	// Write the content of the byte slice
	if n, err = w.Writer.Write(p); err != nil {
		return
	} else {
		nn += n
	}
	return
}

// writeChunk writes the metadata chunk into the PNG file.
func (w *pngWriter) writeChunk(b []byte, name string) (nn int, err error) {
	header := make([]byte, 8)
	binary.BigEndian.PutUint32(header, uint32(len(b)))
	copy(header[4:], name)
	// Calculate CRC checksum for the chunk
	crc := crc32.NewIEEE()
	_, _ = crc.Write(header[4:8])
	_, _ = crc.Write(b)
	footer := make([]byte, 4)
	binary.BigEndian.PutUint32(footer, crc.Sum32())

	// Write the chunk header
	n := 0
	if n, err = w.Writer.Write(header); err != nil {
		return
	} else {
		nn += n
	}
	// Write the chunk metadata
	if n, err = w.Writer.Write(b); err != nil {
		return
	} else {
		nn += n
	}
	// Write the chunk footer
	if n, err = w.Writer.Write(footer); err != nil {
		return
	} else {
		nn += n
	}
	return
}
