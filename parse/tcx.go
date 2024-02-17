package parse

import (
	"io"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/llehouerou/go-tcx"
)

// parseTCX parses text in TCX format from r and returns a slice of activities that pass the selector filter.
// If an error occurs when parsing the TCX data, this error is returned.
func parseTCX(r io.Reader, selector *Selector) ([]*Activity, error) {
	// Parse r to a TCX type struct
	f, err := tcx.Parse(r)
	if err != nil {
		return nil, err
	}

	// Init slice of activities
	acts := make([]*Activity, 0, len(f.Activities))

	// For every activity in the TCX file
	for _, a := range f.Activities {
		// Skip if the activity does not contain any Laps or if the sport is not in the selector filter
		if len(a.Laps) == 0 || !selector.Sport(a.Sport) {
			continue
		}

		// Init Activity
		act := &Activity{
			Sport:   a.Sport,
			Records: make([]*Record, 0, len(a.Laps[0].Track)),
		}

		var t0, t1 tcx.Trackpoint
		for _, l := range a.Laps {
			// Skip if the laps does not contain any GPS points
			if len(l.Track) == 0 {
				continue
			}

			// Add this Lap's distance to the total distance of the Activity
			act.Distance += l.DistanceInMeters

			for _, t := range l.Track {
				// Skip point if either the lat or lon is exactly 0.
				// This usually indicated a GPS measurement error.
				if t.LatitudeInDegrees == 0 || t.LongitudeInDegrees == 0 {
					continue
				}

				// Keep track of the first point
				if len(act.Records) == 0 {
					t0 = t
				}
				// Keep track of the last point
				t1 = t

				// Append the time and position to the activity
				act.Records = append(act.Records, &Record{
					Timestamp: t.Time,
					Position:  geo.NewPointFromDegrees(t.LatitudeInDegrees, t.LongitudeInDegrees),
				})
			}
		}

		// Total duration of Activity
		dur := t1.Time.Sub(t0.Time)

		// Skip if Activity does not have any GPS position or if it fails one of the selector filters
		if len(act.Records) == 0 ||
			!selector.Timestamp(t0.Time, t1.Time) ||
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
