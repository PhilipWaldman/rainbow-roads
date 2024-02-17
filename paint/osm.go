package paint

import (
	"errors"
	"hash/fnv"
	"log"
	"math"
	"math/big"
	"os"
	"path"
	"time"

	"github.com/NathanBaulch/rainbow-roads/geo"
	"github.com/serjvanilla/go-overpass"
	"github.com/vmihailenco/msgpack/v5"
	"golang.org/x/exp/slices"
)

// way represents any kind of road.
type way struct {
	Geometry []geo.Point
	Highway  string
	Access   string
	Surface  string
}

// ttl represents the time-to-live duration for cached OSM data.
const ttl = 168 * time.Hour

// osmLookup performs a lookup for OSM data based on the provided query string.
func osmLookup(query string) ([]*way, error) {
	// Generate a unique filename based on the query string hash.
	h := fnv.New64()
	_, _ = h.Write([]byte(query))
	name := path.Join(os.TempDir(), "rainbow-roads")
	if err := os.MkdirAll(name, 777); err != nil {
		return nil, err
	}
	name = path.Join(name, big.NewInt(0).SetBytes(h.Sum(nil)).Text(62))

	// Check if cached data exists and is still valid.
	if f, err := os.Stat(name); err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	} else if err == nil && time.Now().Sub(f.ModTime()) < ttl {
		if data, err := os.ReadFile(name); err != nil {
			log.Println("WARN:", err)
		} else if ways, err := unpackWays(data); err != nil {
			log.Println("WARN:", err)
		} else {
			return ways, nil
		}
	}

	// Query OSM for data and cache the result.
	if res, err := overpass.Query(query); err != nil {
		return nil, err
	} else if data, err := packWays(res.Ways); err != nil {
		return nil, err
	} else if err := os.WriteFile(name, data, 777); err != nil {
		return nil, err
	} else {
		return unpackWays(data)
	}
}

// packWays serializes a map of ways into a MessagePack byte slice.
// The resulting byte slice contains the serialized MessagePack data.
// If an error occurs during serialization, it returns an error.
func packWays(ways map[int64]*overpass.Way) ([]byte, error) {
	// Create a new doc struct to hold the serialized ways
	d := doc{Ways: make([]elem, len(ways))}

	i := 0
	// Iterate over each way in the input map
	for _, w := range ways {
		// Convert the geometry of the way to radians and store it in the doc
		d.Ways[i].Geometry = make([][2]float32, len(w.Geometry))
		for j, g := range w.Geometry {
			pt := geo.NewPointFromDegrees(g.Lat, g.Lon)
			d.Ways[i].Geometry[j][0] = float32(pt.Lat)
			d.Ways[i].Geometry[j][1] = float32(pt.Lon)
		}

		// Helper function to pack tags and add them to the known slice
		packTag := func(tag string, known *[]string) uint8 {
			if val, ok := w.Tags[tag]; ok {
				// Check if the tag value is already known
				j := slices.Index(*known, val)
				if j < 0 {
					// If not, add it to the known slice
					j = len(*known)
					*known = append(*known, val)
				}
				// Return the index of the value in the known slice
				return uint8(j)
			}
			// Return the maximum value if the tag is not found
			return math.MaxUint8
		}

		// Pack highway, access, and surface tags and update the known slices
		d.Ways[i].Highway = packTag("highway", &d.Highways)
		d.Ways[i].Access = packTag("access", &d.Accesses)
		d.Ways[i].Surface = packTag("surface", &d.Surfaces)

		i++
	}

	// Marshal the doc struct to MessagePack format
	return msgpack.Marshal(d)
}

// unpackWays deserializes the given byte slice containing MessagePack data into a slice of way structs.
// The constructed slice of way structs is returned.
// If an error occurs during unmarshaling or data extraction, it returns nil and the error.
func unpackWays(data []byte) ([]*way, error) {
	// Create a new doc struct to hold the unmarshaled data
	d := &doc{}
	// Unmarshal the MessagePack data into the doc struct
	if err := msgpack.Unmarshal(data, d); err != nil {
		return nil, err
	}

	// Initialize a slice to hold the extracted way structs
	ways := make([]*way, len(d.Ways))
	// Iterate over each way in the doc struct and extract information
	for i, w := range d.Ways {
		// Create a new way struct and initialize its geometry slice
		ways[i] = &way{Geometry: make([]geo.Point, len(w.Geometry))}
		// Convert the geometry data to geo.Points and store them in the way struct
		for j, p := range w.Geometry {
			ways[i].Geometry[j].Lat = float64(p[0])
			ways[i].Geometry[j].Lon = float64(p[1])
		}

		// If the way has a valid highway tag index, assign the corresponding highway tag
		if w.Highway < math.MaxUint8 {
			if w.Highway >= uint8(len(d.Highways)) {
				return nil, errors.New("invalid cache data")
			}
			ways[i].Highway = d.Highways[w.Highway]
		}

		// If the way has a valid access tag index, assign the corresponding access tag
		if w.Access < math.MaxUint8 {
			if w.Access >= uint8(len(d.Accesses)) {
				return nil, errors.New("invalid cache data")
			}
			ways[i].Access = d.Accesses[w.Access]
		}

		// If the way has a valid surface tag index, assign the corresponding surface tag
		if w.Surface < math.MaxUint8 {
			if w.Surface >= uint8(len(d.Surfaces)) {
				return nil, errors.New("invalid cache data")
			}
			ways[i].Surface = d.Surfaces[w.Surface]
		}
	}

	// Return the slice of way structs and nil error if successful
	return ways, nil
}

// doc is a way in a format that can be packed.
type doc struct {
	Ways     []elem   `msgpack:"w"`
	Highways []string `msgpack:"h"`
	Accesses []string `msgpack:"a"`
	Surfaces []string `msgpack:"s"`
}

// elem is a single point of a way and its attributes in a format that can be packed.
type elem struct {
	Geometry [][2]float32 `msgpack:"g"`
	Highway  uint8        `msgpack:"h"`
	Access   uint8        `msgpack:"a"`
	Surface  uint8        `msgpack:"s"`
}
