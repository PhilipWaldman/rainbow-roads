package parse

import (
	"io"
	"strings"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/tkrajina/gpxgo/gpx"
)

// stravaTypeCodes is maps from Strava activity type code to the full name of the activity.
var stravaTypeCodes = map[string]string{
	"1":  "Cycling",
	"2":  "AlpineSkiing",
	"3":  "BackcountrySkiing",
	"4":  "Hiking",
	"5":  "IceSkating",
	"6":  "InlineSkating",
	"7":  "CrossCountrySkiing",
	"8":  "RollerSkiing",
	"9":  "Running",
	"10": "Walking",
	"11": "Workout",
	"12": "Snowboarding",
	"13": "Snowshoeing",
	"14": "Kitesurfing",
	"15": "Windsurfing",
	"16": "Swimming",
	"17": "VirtualBiking",
	"18": "EBiking",
	"19": "Velomobile",
	"21": "Paddling",
	"22": "Kayaking",
	"23": "Rowing",
	"24": "StandUpPaddling",
	"25": "Surfing",
	"26": "Crossfit",
	"27": "Elliptical",
	"28": "RockClimbing",
	"29": "StairStepper",
	"30": "WeightTraining",
	"31": "Yoga",
	"51": "Handcycling",
	"52": "Wheelchair",
	"53": "VirtualRunning",
}

// parseGPX parses text in GPX format from r and returns a slice of activities that pass the selector filter.
// If an error occurs when reading the file or parsing the GPX data, this error is returned.
func parseGPX(r io.Reader, selector *Selector) ([]*Activity, error) {
	// Read all bytes from r
	buf, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}

	// Parse the []byte to a GPX type struct
	g, err := gpx.ParseBytes(buf)
	if err != nil {
		return nil, err
	}

	// Init slice of activities
	acts := make([]*Activity, 0, len(g.Tracks))

	// For every Track (activity) in the GPX file
	for _, t := range g.Tracks {
		// Get the sport
		sport := t.Type
		// If GPX file was created by Strava, convert sport type code to full name
		if strings.Contains(g.Creator, "Strava") {
			if s, ok := stravaTypeCodes[sport]; ok {
				sport = s
			}
		}

		// Skip if this Track has no GPS segments or if the sport is not in the selector filter
		if len(t.Segments) == 0 || !selector.Sport(sport) {
			continue
		}

		// Init Activity
		act := &Activity{
			Sport:   sport,
			Records: make([]*Record, 0, len(t.Segments[0].Points)),
		}

		var p0, p1 gpx.GPXPoint
		for _, s := range t.Segments {
			// Skip if this segment does not contain any points
			if len(s.Points) == 0 {
				continue
			}

			for i, p := range s.Points {
				// Keep track of the first point
				if len(act.Records) == 0 {
					p0 = p
				}
				// Keep track of the last point
				p1 = p

				// Append the time and position to the activity
				act.Records = append(act.Records, &Record{
					Timestamp: p.Timestamp,
					Position:  geo.NewPointFromDegrees(p.Latitude, p.Longitude),
				})

				// Add the distance from the previous to current Record to the total distance of the Activity
				if i > 0 {
					act.Distance += act.Records[i-1].Position.DistanceTo(act.Records[i].Position)
				}
			}
		}

		// Total duration of Activity
		dur := p1.Timestamp.Sub(p0.Timestamp)

		// Skip if Activity does not have any GPS position or if it fails one of the selector filters
		if len(act.Records) == 0 ||
			!selector.Timestamp(p0.Timestamp, p1.Timestamp) ||
			!selector.Duration(dur) ||
			!selector.Distance(act.Distance) ||
			!selector.Pace(dur, act.Distance) {
			continue
		}

		// Append the Activity to the activities slice
		acts = append(acts, act)
	}

	// Return the slice of all valid filtered activities in the file
	return acts, nil
}
