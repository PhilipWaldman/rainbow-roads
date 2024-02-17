package worms

import (
	"archive/zip"
	"bufio"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/gif"
	"io"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/NathanBaulch/rainbow-roads/img"
	"github.com/NathanBaulch/rainbow-roads/parse"
	"github.com/NathanBaulch/rainbow-roads/scan"
	"github.com/StephaneBunel/bresenham"
	"github.com/kettek/apng"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	o          *Options                               // The options to use when painting the image
	fullTitle  string                                 // The text for the watermark in the bottom-right corner
	en         = message.NewPrinter(language.English) // The printer to ouput text to the command line
	files      []*scan.File                           // All the input files
	activities []*parse.Activity                      // The filtered input activities
	maxDur     time.Duration                          // The duration of the longest included activity
	extent     geo.Box                                // A box enclosing all included activities
	images     []*image.Paletted                      // A slice of all the images to animate
)

type Options struct {
	Title       string            // The title of this program
	Version     string            // The version of this program
	Input       []string          // The paths of the input files
	Output      string            // The path of the output file
	Width       uint              // The width of the output image in pixels
	Frames      uint              // The number of animation frames
	FPS         uint              // The framerate the animation
	Format      string            // The output file format string, supports gif, png, zip
	Colors      img.ColorGradient // The color gradient
	ColorDepth  uint              // The number of bits per color in the image palette
	Speed       float64           // How quickly activities progress
	Loop        bool              // If true activities start sequentially and loop continuously; otherwise, all activities start at the same time
	NoWatermark bool              // Whether the watermark is drawn
	Selector    parse.Selector    // The filters specifying which activities to use
}

// Run executes all the steps needed to genetate the worms animation.
func Run(opts *Options) error {
	// Copy the options
	o = opts

	// Construct the full title
	fullTitle = "NathanBaulch/" + o.Title
	if o.Version != "" {
		fullTitle += " " + o.Version
	}

	// If no input was provided, the current directory is the input
	if len(o.Input) == 0 {
		o.Input = []string{"."}
	}

	// Check if the output is valid
	if fi, err := os.Stat(o.Output); err != nil {
		// If invalid path, return an error
		var perr *fs.PathError
		if !errors.As(err, &perr) {
			return err
		}
	} else if fi.IsDir() {
		// If output is a directory, save worms to file named "out"
		o.Output = filepath.Join(o.Output, "out")
	}

	// If no format was specified, extract format from file extension if possible
	ext := filepath.Ext(o.Output)
	if ext != "" {
		ext = ext[1:]
		if o.Format == "" {
			o.Format = ext[1:]
		}
	}

	// If no format was specified, save as gif
	if o.Format == "" {
		o.Format = "gif"
	}

	// If file extension and format differ, save as format
	if !strings.EqualFold(ext, o.Format) {
		o.Output += "." + o.Format
	}

	// Run each stop of the rendering pipeline sequentially
	for _, step := range []func() error{scanStep, parseStep, renderStep, saveStep} {
		if err := step(); err != nil {
			return err
		}
	}

	return nil
}

// scanStep scans the input directory (o.Input) and puts the files in the "files" global variable.
func scanStep() error {
	if f, err := scan.Scan(o.Input); err != nil {
		return err
	} else {
		files = f
		en.Println("files:        ", len(files))
		return nil
	}
}

// parseStep parses the files with the selector filters and puts the filtered activities in the global variable.
func parseStep() error {
	if a, stats, err := parse.Parse(files, &o.Selector); err != nil {
		return err
	} else {
		activities = a
		extent = stats.Extent
		maxDur = stats.MaxDuration
		stats.Print(en)
		return nil
	}
}

// renderStep renders the activity data onto frames for animation.
// It calculates the positions and percentages of activities and generates
// frames based on the provided configuration.
func renderStep() error {
	// Sort activities if looping is enabled to ensure chronological order
	if o.Loop {
		sort.Slice(activities, func(i, j int) bool {
			return activities[i].Records[0].Timestamp.Before(activities[j].Records[0].Timestamp)
		})
	}

	// Calculate map extent and scaling factors
	minX, minY := extent.Min.MercatorProjection()
	maxX, maxY := extent.Max.MercatorProjection()
	dX, dY := maxX-minX, maxY-minY
	scale := float64(o.Width) / dX
	height := uint(dY * scale)
	// Add margins
	scale *= 0.9
	minX -= 0.05 * dX
	maxY += 0.05 * dY
	// Create time scale based off of specified speed and the longest duration
	tScale := 1 / (o.Speed * float64(maxDur))

	// Scale the record positions and percentages by the scale factors
	for i, act := range activities {
		ts0 := act.Records[0].Timestamp
		tOffset := 0.0
		if o.Loop {
			tOffset = float64(i) / float64(len(activities))
		}
		for _, r := range act.Records {
			x, y := r.Position.MercatorProjection()
			r.X = int((x - minX) * scale)
			r.Y = int((maxY - y) * scale)
			r.Percent = tOffset + float64(r.Timestamp.Sub(ts0))*tScale
		}
	}

	// Create the color palette
	pal := color.Palette(make([]color.Color, 1<<o.ColorDepth))
	for i := 0; i < len(pal)-2; i++ {
		pal[i] = o.Colors.GetColorAt(float64(i) / float64(len(pal)-3))
	}
	pal[len(pal)-2] = color.Black
	pal[len(pal)-1] = color.Transparent

	// Initialize all the frames with a background color and optional watermark
	images = make([]*image.Paletted, o.Frames)
	for i := range images {
		im := image.NewPaletted(image.Rect(0, 0, int(o.Width), int(height)), pal)
		if i == 0 {
			drawFill(im, uint8(len(pal)-2))
			if !o.NoWatermark {
				img.DrawWatermark(im, fullTitle, pal[len(pal)/2])
			}
		} else {
			copy(im.Pix, images[0].Pix)
		}
		images[i] = im
	}

	// Create a WaitGroup to wait for all goroutines to finish
	wg := &sync.WaitGroup{}
	wg.Add(int(o.Frames))
	// Process all frames in synchronously
	for f := uint(0); f < o.Frames; f++ {
		f := f
		go func() {
			// Calculate the percentage progress of the current frame in the animation
			fpc := float64(f+1) / float64(o.Frames)
			gp := &glowPlotter{images[f]}
			for _, act := range activities {
				var rPrev *parse.Record
				for _, r := range act.Records {
					// Calculate the percentage progress of the record
					pc := fpc - r.Percent

					// Adjust percentage if it's negative and looping is disabled
					if pc < 0 {
						if !o.Loop {
							break
						}
						pc++
					}

					// Render the line segment if it's different from the previous one
					if rPrev != nil && (r.X != rPrev.X || r.Y != rPrev.Y) {
						// Determine the color index based on the progress
						ci := uint8(len(pal) - 3)
						if pc >= 0 && pc < 1 {
							ci = uint8(math.Sqrt(pc) * float64(len(pal)-2))
						}

						// Draw the line segment
						bresenham.DrawLine(gp, rPrev.X, rPrev.Y, r.X, r.Y, grays[ci])
					}

					// Update the previous record
					rPrev = r
				}
			}
			// Signal the WaitGroup that this goroutine is done
			wg.Done()
		}()
	}
	// Wait for all goroutines to finish
	wg.Wait()

	return nil
}

// saveStep saves the worms to the specified output directory and file name as the specified file type.
func saveStep() error {
	// Create the save directory if it doesn't exist
	if dir := filepath.Dir(o.Output); dir != "." {
		if err := os.MkdirAll(dir, os.ModePerm); err != nil {
			return err
		}
	}

	// Create an empty file
	out, err := os.Create(o.Output)
	if err != nil {
		return err
	}

	// At the very end, ensure the file is closed
	defer func() {
		if err := out.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	// Depending on the save format, save appropriately
	switch o.Format {
	case "gif":
		return saveGIF(out)
	case "png":
		return savePNG(out)
	case "zip":
		return saveZIP(out)
	default:
		return nil
	}
}

// saveGIF save the worms to w as a gif.
func saveGIF(w io.Writer) error {
	// Optimize frames to reduce file size
	optimizeFrames(images)

	// Initialize gif
	g := &gif.GIF{
		Image:    images,
		Delay:    make([]int, len(images)),
		Disposal: make([]byte, len(images)),
		Config: image.Config{
			ColorModel: images[0].Palette,
			Width:      images[0].Rect.Max.X,
			Height:     images[0].Rect.Max.Y,
		},
	}

	// Convert "frames per second" to "100s of seconds per frame"
	d := int(math.Round(100 / float64(o.FPS)))

	// Set delay and disposal method of each frame
	for i := range images {
		g.Disposal[i] = gif.DisposalNone
		g.Delay[i] = d
	}

	// Save all the frames of the gif to the file
	return gif.EncodeAll(&gifWriter{Writer: bufio.NewWriter(w), Comment: fullTitle}, g)
}

// saveGIF save the worms to w as a png.
func savePNG(w io.Writer) error {
	// Optimize frames to reduce file size
	optimizeFrames(images)

	// Initialize png
	a := apng.APNG{Frames: make([]apng.Frame, len(images))}

	// Set each frame
	for i, im := range images {
		a.Frames[i].Image = im
		a.Frames[i].XOffset = im.Rect.Min.X
		a.Frames[i].YOffset = im.Rect.Min.Y
		a.Frames[i].BlendOp = apng.BLEND_OP_OVER
		a.Frames[i].DelayNumerator = 1
		a.Frames[i].DelayDenominator = uint16(o.FPS)
	}

	// Save the apng to the file
	return apng.Encode(&pngWriter{Writer: w, Text: fullTitle}, a)
}

// saveGIF save the worms to w as a zip of gifs.
func saveZIP(w io.Writer) error {
	z := zip.NewWriter(w)

	// At the very end, ensure the file is closed
	defer func() {
		if err := z.Close(); err != nil {
			log.Fatal(err)
		}
	}()

	// Add every image to the zip as a gif
	for i, im := range images {
		if w, err := z.Create(fmt.Sprintf("%d.gif", i)); err != nil {
			return err
		} else if err = gif.Encode(w, im, nil); err != nil {
			return err
		}
	}
	return nil
}
