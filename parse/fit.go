package parse

import (
	"errors"
	"io"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/tormoder/fit"
)

// parseFIT parses text in FIT format from r.
// Since FIT files only contain a single activity, the returned []*Activity will always have a length of 1.
// If the activity does not satisfy the selector filter, nil is returned.
// If an error occurs when parsing the FIT data, this error is returned.
func parseFIT(r io.Reader, selector *Selector) ([]*Activity, error) {
	// Parse the FIT file
	f, err := fit.Decode(r)
	if err != nil {
		var ferr fit.FormatError
		if errors.As(err, &ferr) {
			return nil, nil
		}
		return nil, err
	}

	// Return nil if the FIT file is not an activity or if it contains no GPS Records
	if a, err := f.Activity(); err != nil || len(a.Records) == 0 {
		return nil, nil
	} else {
		// Set Activity sport and total distance
		act := &Activity{
			Sport:    a.Sessions[0].Sport.String(),
			Distance: a.Sessions[0].GetTotalDistanceScaled(),
		}

		// Get the first and last Records
		r0, r1 := a.Records[0], a.Records[len(a.Records)-1]
		// Calc total duration
		dur := r1.Timestamp.Sub(r0.Timestamp)
		// Return nil if the activity does not satisfy the selector filter
		if !selector.Sport(act.Sport) ||
			!selector.Timestamp(r0.Timestamp, r1.Timestamp) ||
			!selector.Duration(dur) ||
			!selector.Distance(act.Distance) ||
			!selector.Pace(dur, act.Distance) {
			return nil, nil
		}

		act.Records = make([]*Record, 0, len(a.Records))
		// For every Record
		for _, rec := range a.Records {
			// If it is valid, append it to the Activity
			if !rec.PositionLat.Invalid() && !rec.PositionLong.Invalid() {
				act.Records = append(act.Records, &Record{
					Timestamp: rec.Timestamp,
					Position:  geo.NewPointFromSemicircles(rec.PositionLat.Semicircles(), rec.PositionLong.Semicircles()),
				})
			}
		}

		// If the activity does not contain any records, return nil
		if len(act.Records) == 0 {
			return nil, nil
		}

		// Return the activity as a singleton slice
		return []*Activity{act}, nil
	}
}
