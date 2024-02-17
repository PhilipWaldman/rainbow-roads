package parse

import (
	"errors"
	"fmt"
	"io"
	"math"
	"os"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/NathanBaulch/rainbow-roads/scan"
	"golang.org/x/exp/slices"
	"golang.org/x/text/message"
)

// Parse parses the files and filters the activities with selector.
// The activities are returned together with the Stats over all activities.
// An error is returned if anything goes wrong.
func Parse(files []*scan.File, selector *Selector) ([]*Activity, *Stats, error) {
	// Read and parse all files in parallel.
	// The result, either a slice of Activities or an error is saved in res
	wg := sync.WaitGroup{}
	wg.Add(len(files))
	res := make([]struct {
		acts []*Activity
		err  error
	}, len(files))
	for i := range files {
		i := i
		go func() {
			defer wg.Done()
			var parser func(io.Reader, *Selector) ([]*Activity, error)
			switch files[i].Ext {
			case ".fit":
				parser = parseFIT
			case ".gpx":
				parser = parseGPX
			case ".tcx":
				parser = parseTCX
			default:
				return
			}
			if r, err := files[i].Opener(); err != nil {
				res[i].err = err
			} else {
				res[i].acts, res[i].err = parser(r, selector)
			}
		}()
	}
	wg.Wait()

	// print a warning for every file that was not parsed correctly,
	// otherwise append it to an Activity slice.
	activities := make([]*Activity, 0, len(files))
	for _, r := range res {
		if r.err != nil {
			fmt.Fprintln(os.Stderr, "WARN:", r.err)
		} else {
			activities = append(activities, r.acts...)
		}
	}
	// If not activities were (successfully) parsed, return an error
	if len(activities) == 0 {
		return nil, nil, errors.New("no matching activities found")
	}

	// Init stats with default (extreme) values
	stats := &Stats{
		SportCounts: make(map[string]int),
		After:       time.UnixMilli(math.MaxInt64),
		MinDuration: time.Duration(math.MaxInt64),
		MinDistance: math.MaxFloat64,
		MinPace:     time.Duration(math.MaxInt64),
	}
	var startExtent, endExtent geo.Box

	uniq := make(map[time.Time]bool)

	// Filters activities with selector.
	// Removes duplicate activities.
	// Summarizes all activities to stats.
	for i := len(activities) - 1; i >= 0; i-- {
		act := activities[i]
		include := selector.PassesThrough.IsZero()
		exclude := len(act.Records) == 0
		for j, r := range act.Records {
			if !selector.Bounded(r.Position) {
				exclude = true
				break
			}
			if j == 0 && !selector.Starts(r.Position) {
				exclude = true
				break
			}
			if j == len(act.Records)-1 && !selector.Ends(r.Position) {
				exclude = true
				break
			}
			if !include && selector.Passes(r.Position) {
				include = true
			}
		}
		if exclude || !include || uniq[act.Records[0].Timestamp] {
			j := len(activities) - 1
			activities[i] = activities[j]
			activities = activities[:j]
			continue
		}
		uniq[act.Records[0].Timestamp] = true

		if act.Sport == "" {
			stats.SportCounts["unknown"]++
		} else {
			stats.SportCounts[strings.ToLower(act.Sport)]++
		}
		ts0, ts1 := act.Records[0].Timestamp, act.Records[len(act.Records)-1].Timestamp
		if ts0.Before(stats.After) {
			stats.After = ts0
		}
		if ts1.After(stats.Before) {
			stats.Before = ts1
		}
		dur := ts1.Sub(ts0)
		if dur < stats.MinDuration {
			stats.MinDuration = dur
		}
		if dur > stats.MaxDuration {
			stats.MaxDuration = dur
		}
		if act.Distance < stats.MinDistance {
			stats.MinDistance = act.Distance
		}
		if act.Distance > stats.MaxDistance {
			stats.MaxDistance = act.Distance
		}
		pace := time.Duration(float64(dur) / act.Distance)
		if pace < stats.MinPace {
			stats.MinPace = pace
		}
		if pace > stats.MaxPace {
			stats.MaxPace = pace
		}

		stats.CountRecords += len(act.Records)
		stats.SumDuration += dur
		stats.SumDistance += act.Distance

		for _, r := range act.Records {
			stats.Extent = stats.Extent.Enclose(r.Position)
		}
		startExtent = startExtent.Enclose(act.Records[0].Position)
		endExtent = endExtent.Enclose(act.Records[len(act.Records)-1].Position)
	}

	// If no activities remain, return an error
	if len(activities) == 0 {
		return nil, nil, errors.New("no matching activities found")
	}

	// Finish stats
	stats.CountActivities = len(activities)
	stats.BoundedBy = geo.Circle{Origin: stats.Extent.Center()}
	stats.StartsNear = geo.Circle{Origin: startExtent.Center()}
	stats.EndsNear = geo.Circle{Origin: endExtent.Center()}
	for _, act := range activities {
		for _, r := range act.Records {
			stats.BoundedBy = stats.BoundedBy.Enclose(r.Position)
		}
		stats.StartsNear = stats.StartsNear.Enclose(act.Records[0].Position)
		stats.EndsNear = stats.EndsNear.Enclose(act.Records[len(act.Records)-1].Position)
	}

	return activities, stats, nil
}

// Selector defines criteria for selecting activities based on various parameters.
// It includes information about sports, time, duration, distance, pace, and geographic locations.
type Selector struct {
	Sports        []string      // Sports represents the list of sports to filter activities.
	After         time.Time     // After is the earliest activities may occur.
	Before        time.Time     // Before is the latest activities may occur.
	MinDuration   time.Duration // MinDuration specifies the minimum duration of activities.
	MaxDuration   time.Duration // MaxDuration specifies the maximum duration of activities.
	MinDistance   float64       // MinDistance specifies the minimum distance of activities.
	MaxDistance   float64       // MaxDistance specifies the maximum distance of activities.
	MinPace       time.Duration // MinPace specifies the minimum pace of activities.
	MaxPace       time.Duration // MaxPace specifies the maximum pace of activities.
	BoundedBy     geo.Circle    // BoundedBy specifies a Circle that activities must completely lay within.
	StartsNear    geo.Circle    // StartsNear specifies a Circle that the starting points of activities must lay within.
	EndsNear      geo.Circle    // EndsNear specifies a Circle that the ending points of activities must lay within.
	PassesThrough geo.Circle    // PassesThrough specifies a Circle that activities must pass through.
}

// Sport checks if the given sport is included in the Selector's sports list.
func (s *Selector) Sport(sport string) bool {
	return len(s.Sports) == 0 || slices.IndexFunc(s.Sports, func(s string) bool { return strings.EqualFold(s, sport) }) >= 0
}

// Timestamp checks if the activity's timestamp falls within the time range specified by Selector.
func (s *Selector) Timestamp(from, to time.Time) bool {
	return (s.After.IsZero() || s.After.Before(from)) && (s.Before.IsZero() || s.Before.After(to))
}

// Duration checks if the activity's duration falls within the duration range specified by Selector.
func (s *Selector) Duration(duration time.Duration) bool {
	return duration > 0 &&
		(s.MinDuration == 0 || duration > s.MinDuration) &&
		(s.MaxDuration == 0 || duration < s.MaxDuration)
}

// Distance checks if the activity's distance falls within the distance range specified by Selector.
func (s *Selector) Distance(distance float64) bool {
	return distance > 0 &&
		(s.MinDistance == 0 || distance > s.MinDistance) &&
		(s.MaxDistance == 0 || distance < s.MaxDistance)
}

// Pace checks if the activity's pace falls within the pace range specified by Selector.
func (s *Selector) Pace(duration time.Duration, distance float64) bool {
	pace := time.Duration(float64(duration) / distance)
	return pace > 0 &&
		(s.MinPace == 0 || pace > s.MinPace) &&
		(s.MaxPace == 0 || pace < s.MaxPace)
}

// Bounded checks if the activity falls within the bounding area specified by Selector.
func (s *Selector) Bounded(pt geo.Point) bool {
	return s.BoundedBy.IsZero() || s.BoundedBy.Contains(pt)
}

// Starts checks if the activity starts near the specified point by Selector.
func (s *Selector) Starts(pt geo.Point) bool {
	return s.StartsNear.IsZero() || s.StartsNear.Contains(pt)
}

// Ends checks if the activity ends near the specified point by Selector.
func (s *Selector) Ends(pt geo.Point) bool {
	return s.EndsNear.IsZero() || s.EndsNear.Contains(pt)
}

// Passes checks if the activity passes through the specified point by Selector.
func (s *Selector) Passes(pt geo.Point) bool {
	return s.PassesThrough.IsZero() || s.PassesThrough.Contains(pt)
}

// Activity represents an activity with its sport, distance, and records.
type Activity struct {
	Sport    string    // Sport represents the type of sport for the activity.
	Distance float64   // Distance represents the distance covered in the activity.
	Records  []*Record // Records represents the records associated with the activity.
}

// Record represents a record of an activity including timestamp, position, coordinates, and percent.
type Record struct {
	Timestamp time.Time // Timestamp represents the time when the record was made.
	Position  geo.Point // Position represents the geographical position associated with the record.
	X         int       // X is the x-coordinate of the record.
	Y         int       // Y is the y-coordinate of the record.
	Percent   float64   // Percent represents a percentage associated with the record.
}

// Stats contains statistics aggregated from activities and records.
type Stats struct {
	CountActivities int            // CountActivities represents the number of activities.
	CountRecords    int            // CountRecords represents the number of records.
	SportCounts     map[string]int // SportCounts contains counts of activities per sport.
	After           time.Time      // After is the earliest time of an activity.
	Before          time.Time      // Before is the latest time of an activity.
	MinDuration     time.Duration  // MinDuration is the duration of the shortest duration activity.
	MaxDuration     time.Duration  // MaxDuration is the duration of the longest duration activity.
	SumDuration     time.Duration  // SumDuration is the duration of all activities combined.
	MinDistance     float64        // MinDistance is the distance of the shortest distance activity.
	MaxDistance     float64        // MaxDistance is the distance of the longest distance activity.
	SumDistance     float64        // SumDistance is the distance of all activities combined.
	MinPace         time.Duration  // MinPace is the slowest pace.
	MaxPace         time.Duration  // MaxPace is the fastest pace.
	BoundedBy       geo.Circle     // BoundedBy is a Circle enclosing all activities.
	StartsNear      geo.Circle     // StartsNear is a Circle enclosing the starting point of all activities.
	EndsNear        geo.Circle     // EndsNear is a Circle enclosing the ending point of all activities.
	Extent          geo.Box        // Extent is a Box enclosing all activities.
}

// Print prints statistics information using a given printer.
func (s *Stats) Print(p *message.Printer) {
	avgDur := s.SumDuration / time.Duration(s.CountActivities)
	avgDist := s.SumDistance / float64(s.CountActivities)
	avgPace := s.SumDuration / time.Duration(s.SumDistance)

	p.Printf("activities:    %d\n", s.CountActivities)
	p.Printf("records:       %d\n", s.CountRecords)
	p.Printf("sports:        %s\n", sprintSportStats(p, s.SportCounts))
	p.Printf("period:        %s\n", sprintPeriod(p, s.After, s.Before))
	p.Printf("duration:      %s to %s, average %s, total %s\n", sprintDuration(p, s.MinDuration), sprintDuration(p, s.MaxDuration), sprintDuration(p, avgDur), sprintDuration(p, s.SumDuration))
	p.Printf("distance:      %s to %s, average %s, total %s\n", sprintDistance(p, s.MinDistance), sprintDistance(p, s.MaxDistance), sprintDistance(p, avgDist), sprintDistance(p, s.SumDistance))
	p.Printf("pace:          %s to %s, average %s\n", sprintPace(p, s.MinPace), sprintPace(p, s.MaxPace), sprintPace(p, avgPace))
	p.Printf("bounds:        %s\n", s.BoundedBy)
	p.Printf("starts within: %s\n", s.StartsNear)
	p.Printf("ends within:   %s\n", s.EndsNear)
}

// sprintSportStats formats sports statistics into a string using the given printer.
func sprintSportStats(p *message.Printer, stats map[string]int) string {
	// Convert the map into a slice of key-value pairs for sorting
	pairs := make([]struct {
		k string
		v int
	}, len(stats))
	i := 0
	for k, v := range stats {
		pairs[i].k = k
		pairs[i].v = v
		i++
	}
	// Sort the key-value pairs based on the count of occurrences
	sort.Slice(pairs, func(i, j int) bool {
		p0, p1 := pairs[i], pairs[j]
		return p0.v > p1.v || (p0.v == p1.v && p0.k < p1.k)
	})
	// Prepare the array for formatted output
	a := make([]any, len(stats)*2)
	i = 0
	// Populate the array with keys and values
	for _, kv := range pairs {
		a[i] = kv.k
		i++
		a[i] = kv.v
		i++
	}
	// Format the output string using the printer
	return p.Sprintf(strings.Repeat(", %s (%d)", len(stats))[2:], a...)
}

// sprintPeriod formats the period between two dates into a string using the given printer.
func sprintPeriod(p *message.Printer, minDate, maxDate time.Time) string {
	// Calculate the duration between minDate and maxDate
	d := maxDate.Sub(minDate)
	var num float64
	var unit string
	// Determine the appropriate unit for the duration
	switch {
	case d.Hours() >= 365.25*24:
		num, unit = d.Hours()/(365.25*24), "years"
	case d.Hours() >= 365.25*2:
		num, unit = d.Hours()/(365.25*2), "months"
	case d.Hours() >= 7*24:
		num, unit = d.Hours()/(7*24), "weeks"
	case d.Hours() >= 24:
		num, unit = d.Hours()/24, "days"
	case d.Hours() >= 1:
		num, unit = d.Hours(), "hours"
	case d.Minutes() >= 1:
		num, unit = d.Minutes(), "minutes"
	default:
		num, unit = d.Seconds(), "seconds"
	}
	// Format and return the period string
	return p.Sprintf("%.1f %s (%s to %s)", num, unit, minDate.Format("2006-01-02"), maxDate.Format("2006-01-02"))
}

// sprintDuration formats the duration into a string using the given printer.
// The duration is in seconds.
func sprintDuration(p *message.Printer, dur time.Duration) string {
	return p.Sprintf("%s", dur.Truncate(time.Second))
}

// sprintDistance formats the distance into a string using the given printer.
// The distance is in kilometers.
func sprintDistance(p *message.Printer, dist float64) string {
	return p.Sprintf("%.1fkm", dist/1000)
}

// sprintPace formats the pace into a string using the given printer.
// The pace is formatted as seconds per kilometer.
func sprintPace(p *message.Printer, pace time.Duration) string {
	return p.Sprintf("%s/km", (pace * 1000).Truncate(time.Second))
}
