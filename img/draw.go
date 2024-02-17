package img

import (
	"image"
	"image/color"
	"image/draw"

	"golang.org/x/image/font"
	"golang.org/x/image/font/basicfont"
	"golang.org/x/image/math/fixed"
)

// DrawWatermark draws text in the bottom right corner of im in color c.
func DrawWatermark(im image.Image, text string, c color.Color) {
	// Init Drawer
	d := &font.Drawer{
		Dst:  im.(draw.Image),
		Src:  image.NewUniform(c),
		Face: basicfont.Face7x13,
	}
	// Get bounding box of the text
	b, _ := d.BoundString(text)
	b = b.Sub(b.Min)
	// If the text fits in the image
	if b.In(fixed.R(0, 0, im.Bounds().Max.X-10, im.Bounds().Max.Y-10)) {
		d.Dot = fixed.P(im.Bounds().Max.X, im.Bounds().Max.Y). // bottom right corner
									Sub(b.Max.Sub(fixed.P(0, basicfont.Face7x13.Height))). // shift to fit text
									Sub(fixed.P(5, 5))                                     // add margins
		// Add text to image
		d.DrawString(text)
	}
}
