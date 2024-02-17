package paint

import (
	"errors"
	"image"
	"image/color"
	"image/png"
	"io/fs"
	"log"
	"math"
	"os"
	"path/filepath"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/NathanBaulch/rainbow-roads/img"
	"github.com/NathanBaulch/rainbow-roads/parse"
	"github.com/NathanBaulch/rainbow-roads/scan"
	"github.com/antonmedv/expr"
	"github.com/fogleman/gg"
	"golang.org/x/image/colornames"
	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

var (
	o          *Options                               // The options to use when painting the image
	fullTitle  string                                 // The text for the watermark in the bottom-right corner
	en         = message.NewPrinter(language.English) // The printer to ouput text to the command line
	files      []*scan.File                           // All the input files
	activities []*parse.Activity                      // The filtered input activities
	roads      []*way                                 // The roads in the specified region downloaded from OSM
	im         image.Image                            // The generated image

	backCol    = colornames.Black   // The background color
	donePriCol = colornames.Lime    // The primairy color for roads that have been traveled
	doneSecCol = colornames.Green   // the secondairy color for roads that have been traveled
	pendPriCol = colornames.Red     // The primairy color for roads that have not been traveled
	pendSecCol = colornames.Darkred // the secondairy color for roads that have not been traveled
	actCol     = colornames.Blue    // The color of the activity paths
	// queryExpr specifies the expression used for querying certain highway tags
	queryExpr = "is_tag(highway)" +
		" and highway not in ['proposed','corridor','construction','footway','steps','busway','elevator','services']" +
		" and service not in ['driveway','parking_aisle']" +
		" and area != 'yes'"
	// primaryExpr specifies the expression used for primary highway tags
	primaryExpr = mustCompile(
		"highway in ['cycleway','primary','residential','secondary','tertiary','trunk','living_street','unclassified']"+
			" and access not in ['private','customers','no']"+
			" and surface not in ['cobblestone','sett']", expr.AsBool())
)

type Options struct {
	Title       string         // The title of this program
	Version     string         // The version of this program
	Input       []string       // The paths of the input files
	Output      string         // The path of the ouput file
	Width       uint           // The width of the output image in pixels
	Region      geo.Circle     // The region to load the map of
	NoWatermark bool           // Whether the watermark is drawn
	Selector    parse.Selector // The filters specifying which activities to use
	Minimalist  bool           // Whether to only draw the activity paths
}

// Run executes all the steps needed to genetate the image.
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
		// If output is a directory, save image to file named "out"
		o.Output = filepath.Join(o.Output, "out")
	}

	// If the output has no file extension, add ".png" to the output
	if filepath.Ext(o.Output) == "" {
		o.Output += ".png"
	}

	// Run each stop of the rendering pipeline sequentially
	if o.Minimalist {
		// Only draws the activities
		for _, step := range []func() error{scanStep, parseStep, renderStep, saveStep} {
			if err := step(); err != nil {
				return err
			}
		}
	} else {
		// Draws the activities and the map of streets
		for _, step := range []func() error{scanStep, parseStep, fetchStep, renderStep, saveStep} {
			if err := step(); err != nil {
				return err
			}
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
		stats.Print(en)
		return nil
	}
}

// fetchStep downloads the roads from OSM that are in the specified region.
func fetchStep() error {
	query, err := buildQuery(o.Region.Grow(1/0.9), queryExpr)
	if err != nil {
		return err
	}

	roads, err = osmLookup(query)
	return err
}

// renderStep renders the map image based on the provided options and data.
// It generates the map using geographic information and activity paths.
// The rendered image includes different road types and activity paths.
// It also calculates the progress and displays it as a percentage.
func renderStep() error {
	// Calculate origin coordinates and scale for rendering
	oX, oY := o.Region.Origin.MercatorProjection()
	scale := math.Cos(o.Region.Origin.Lat) * 0.9 * float64(o.Width) / (2 * o.Region.Radius)

	// drawLine draws a line on the graphics context based on a geographic point
	drawLine := func(gc *gg.Context, pt geo.Point) {
		x, y := pt.MercatorProjection()
		x = float64(o.Width)/2 + (x-oX)*scale
		y = float64(o.Width)/2 - (y-oY)*scale
		gc.LineTo(x, y)
	}

	// drawActs draws activity paths on the graphics context with a specified line width
	drawActs := func(gc *gg.Context, lineWidth float64) {
		gc.SetLineWidth(1.3 * lineWidth * scale)
		for _, a := range activities {
			for _, r := range a.Records {
				drawLine(gc, r.Position)
			}
			gc.Stroke()
		}
	}

	// Initialize the graphics context for drawing the map
	gc := gg.NewContext(int(o.Width), int(o.Width))
	gc.SetFillStyle(gg.NewSolidPattern(backCol))
	gc.DrawRectangle(0, 0, float64(o.Width), float64(o.Width))
	gc.Fill()

	// Draw activity paths on the graphics context
	gc.SetStrokeStyle(gg.NewSolidPattern(actCol))
	drawActs(gc, 10)

	// drawWays draws roads on the graphics context based on their status (primary or secondary)
	drawWays := func(primary bool, strokeColor color.Color) {
		gc.SetStrokeStyle(gg.NewSolidPattern(strokeColor))

		for _, w := range roads {
			if !primary || mustRun(primaryExpr, (*wayEnv)(w)).(bool) {
				lineWidth := 10.0
				switch w.Highway {
				case "motorway", "trunk", "primary", "secondary", "tertiary":
					lineWidth *= 3.6
				case "motorway_link", "trunk_link", "primary_link", "secondary_link", "tertiary_link", "residential", "living_street":
					lineWidth *= 2.4
				case "pedestrian", "footway", "cycleway", "track":
					lineWidth *= 1.4
				}
				gc.SetLineWidth(lineWidth * scale)
				for _, pt := range w.Geometry {
					drawLine(gc, pt)
				}
				gc.Stroke()
			}
		}
	}

	// Create a mask graphics context for drawing the road colors
	maskGC := gg.NewContext(int(o.Width), int(o.Width))
	drawActs(maskGC, 50)
	actMask := maskGC.AsMask()

	// Draw secondary roads
	_ = gc.SetMask(actMask)
	drawWays(false, doneSecCol)
	gc.InvertMask()
	drawWays(false, pendSecCol)

	// Draw primary roads
	_ = maskGC.SetMask(actMask)
	maskGC.SetColor(color.Transparent)
	maskGC.Clear()
	maskGC.SetColor(color.Black)
	maskGC.DrawCircle(float64(o.Width)/2, float64(o.Width)/2, 0.9*float64(o.Width)/2)
	maskGC.Fill()
	_ = gc.SetMask(maskGC.AsMask())
	drawWays(true, pendPriCol)

	// Invert the mask for drawing done primary roads
	maskGC.InvertMask()
	maskGC.SetColor(color.Transparent)
	maskGC.Clear()
	maskGC.SetColor(color.Black)
	maskGC.DrawCircle(float64(o.Width)/2, float64(o.Width)/2, 0.9*float64(o.Width)/2)
	maskGC.Fill()
	_ = gc.SetMask(maskGC.AsMask())
	drawWays(true, donePriCol)

	// Draw watermark if not disabled
	if !o.NoWatermark {
		img.DrawWatermark(gc.Image(), fullTitle, pendSecCol)
	}

	// Calculate and print progress
	done, pend := 0, 0
	rect := gc.Image().Bounds()
	for y := rect.Min.Y; y <= rect.Max.Y; y++ {
		for x := rect.Min.X; x <= rect.Max.X; x++ {
			switch gc.Image().At(x, y) {
			case donePriCol:
				done++
			case pendPriCol:
				pend++
			}
		}
	}
	if done == 0 && pend == 0 {
		pend = 1
	}
	en.Printf("progress:      %.2f%%\n", 100*float64(done)/float64(done+pend))

	im = gc.Image() // Set the rendered image
	return nil
}

// wayEnv is an extension of way that implements a Fetch function.
type wayEnv way

// Fetch returns the type of highway, access type, and surface material of the wayEnv e
// when given the string parameters "highway", "access", and "surface", respectively.
func (e *wayEnv) Fetch(k any) any {
	switch k.(string) {
	case "highway":
		return e.Highway
	case "access":
		return e.Access
	case "surface":
		return e.Surface
	}
	return nil
}

// saveStep saves the image to the specified output directory and file name as a png.
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

	// Save the image to the file
	return png.Encode(out, im)
}
