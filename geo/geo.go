package geo

import (
	"fmt"
	"math"

	"github.com/NathanBaulch/rainbow-roads/conv"
)

const (
	// The semi-major axis of WGS 84 in meters
	mercatorRadius = 6_378_137
	// The global average radius of Earth in meters.
	haversineRadius = 6_371_000
)

// DegreesToRadians converts d from degree to radians.
func DegreesToRadians(d float64) float64 {
	return d * math.Pi / 180
}

// RadiansToDegrees converts r from radians to degrees.
func RadiansToDegrees(r float64) float64 {
	return r * 180 / math.Pi
}

// SemicirclesToRadians converts s from semicircle units [math.MinInt32, math.MaxInt32] to radians.
func SemicirclesToRadians(s int32) float64 {
	return float64(s) * math.Pi / math.MaxInt32
}

// NewPointFromDegrees returns a new Point from lat and lon, where lat and lon are in degrees.
func NewPointFromDegrees(lat, lon float64) Point {
	return Point{Lat: DegreesToRadians(lat), Lon: DegreesToRadians(lon)}
}

// NewPointFromSemicircles returns a new Point from lat and lon, where lat and lon are in semicircles units.
func NewPointFromSemicircles(lat, lon int32) Point {
	return Point{Lat: SemicirclesToRadians(lat), Lon: SemicirclesToRadians(lon)}
}

// A Point on Earth represented by a latitude Lat and longitude Lon, both in radians.
type Point struct {
	Lat, Lon float64
}

// The String representating of p in the format "lat,lon", where lat and lon are converted into degrees.
func (p Point) String() string {
	return fmt.Sprintf("%s,%s", conv.FormatFloat(RadiansToDegrees(p.Lat)), conv.FormatFloat(RadiansToDegrees(p.Lon)))
}

// IsZero returns true is both p.Lat and p.Lon are 0.
func (p Point) IsZero() bool {
	return p.Lat == 0 && p.Lon == 0
}

// DistanceTo calculates the haversine distance (in meters) between Point p and Point pt on Earth's surface.
func (p Point) DistanceTo(pt Point) float64 {
	sinLat := math.Sin((pt.Lat - p.Lat) / 2)
	sinLon := math.Sin((pt.Lon - p.Lon) / 2)
	a := sinLat*sinLat + math.Cos(p.Lat)*math.Cos(pt.Lat)*sinLon*sinLon
	return 2 * haversineRadius * math.Atan2(math.Sqrt(a), math.Sqrt(1-a))
}

// MercatorProjection calculates where Point p would fall on a Mercator projection.
func (p Point) MercatorProjection() (float64, float64) {
	x := mercatorRadius * p.Lon
	y := mercatorRadius * math.Log(math.Tan((2*p.Lat+math.Pi)/4))
	return x, y
}

// A Circle represented by its center Origin and a Radius.
type Circle struct {
	Origin Point
	Radius float64
}

// String returns Circle c as a string of format "Origin,Radius"
func (c Circle) String() string {
	return fmt.Sprintf("%s,%s", c.Origin, conv.FormatFloat(c.Radius))
}

// IsZero returns true is the Radius of c is 0; otherwise, false.
func (c Circle) IsZero() bool {
	return c.Radius == 0
}

// Contains returns true if Point pt is within Circle c.
func (c Circle) Contains(pt Point) bool {
	return c.Origin.DistanceTo(pt) < c.Radius
}

// Enclose returns a Circle c with the smallest Radius >= to c.Radius such that Point pt is within c.
func (c Circle) Enclose(pt Point) Circle {
	c.Radius = math.Max(c.Radius, c.Origin.DistanceTo(pt))
	return c
}

// Grow returns a Circle that is factor times larger than c.
func (c Circle) Grow(factor float64) Circle {
	c.Radius *= factor
	return c
}

// Box is a grid alligned rectangle represented by 2 Points.
// Min is the corner with the smallest Lat and Lon and
// Max is the corner with the largest Lat and Lon.
type Box struct {
	Min, Max Point
}

// IsZero returns true is both b.Min and b.Max are zero.
func (b Box) IsZero() bool {
	return b.Min.IsZero() && b.Max.IsZero()
}

// Center returns a Point of the center of the Box.
func (b Box) Center() Point {
	return Point{Lat: (b.Max.Lat + b.Min.Lat) / 2, Lon: (b.Max.Lon + b.Min.Lon) / 2}
}

// Enclose returns the smallest Box >= b such that Point pt is within the Box.
func (b Box) Enclose(pt Point) Box {
	if b.IsZero() {
		b.Min = pt
		b.Max = pt
	} else {
		b.Min.Lat = math.Min(b.Min.Lat, pt.Lat)
		b.Max.Lat = math.Max(b.Max.Lat, pt.Lat)
		b.Min.Lon = math.Min(b.Min.Lon, pt.Lon)
		b.Max.Lon = math.Max(b.Max.Lon, pt.Lon)
	}
	return b
}
