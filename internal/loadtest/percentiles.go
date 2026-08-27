package loadtest

import (
	"math"
	"sort"
)

// CalculatePercentiles calculates min, avg, p50, p90, p95, p99, and max for a slice of float64 latencies in milliseconds.
func CalculatePercentiles(latencies []float64) Percentiles {
	n := len(latencies)
	if n == 0 {
		return Percentiles{}
	}

	sorted := make([]float64, n)
	copy(sorted, latencies)
	sort.Float64s(sorted)

	var sum float64
	for _, v := range sorted {
		sum += v
	}
	avg := sum / float64(n)

	return Percentiles{
		MinMs: round2(sorted[0]),
		AvgMs: round2(avg),
		P50Ms: round2(getPercentile(sorted, 50)),
		P90Ms: round2(getPercentile(sorted, 90)),
		P95Ms: round2(getPercentile(sorted, 95)),
		P99Ms: round2(getPercentile(sorted, 99)),
		MaxMs: round2(sorted[n-1]),
	}
}

func getPercentile(sorted []float64, p float64) float64 {
	n := len(sorted)
	if n == 0 {
		return 0
	}
	if n == 1 {
		return sorted[0]
	}
	rank := (p / 100.0) * float64(n-1)
	lower := int(math.Floor(rank))
	upper := int(math.Ceil(rank))
	weight := rank - float64(lower)
	return sorted[lower]*(1.0-weight) + sorted[upper]*weight
}

func round2(val float64) float64 {
	return math.Round(val*100) / 100
}
