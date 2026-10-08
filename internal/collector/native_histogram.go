package collector

import "time"

// Native-histogram settings shared by every observed (non-const) histogram the
// exporter owns (OPN-0118). Setting NativeHistogramBucketFactor alongside classic
// Buckets makes client_golang keep both representations: the text exposition on
// /metrics still carries the classic _bucket series, while the OTLP Prometheus bridge
// sees the native buckets and exports a base2 exponential histogram, one series per
// label set instead of one per bucket.
//
// Const histograms (unbound recursion time, flow source-byte delta ratio) are built
// from pre-bucketed data and stay classic: their bucket bounds are the data.
const (
	// 1.1 resolves to schema 3: adjacent bucket bounds differ by about 9%.
	nativeHistogramBucketFactor = 1.1
	// Caps per-series memory and payload; past this client_golang widens the
	// buckets (lowers the schema) instead of adding more.
	nativeHistogramMaxBucketNumber uint32 = 160
	// When the bucket cap is hit, reset at most hourly rather than widening
	// immediately, so a long-lived series keeps its resolution.
	nativeHistogramMinResetDuration = time.Hour
)
