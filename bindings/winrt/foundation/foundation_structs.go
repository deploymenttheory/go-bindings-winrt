//go:build windows && (amd64 || arm64)

package foundation

// DateTime is Windows.Foundation.DateTime: a point in time as 100ns ticks
// since January 1, 1601 (UTC).
type DateTime struct {
	UniversalTime int64
}

// TimeSpan is Windows.Foundation.TimeSpan: a duration in 100ns ticks.
type TimeSpan struct {
	Duration int64
}
