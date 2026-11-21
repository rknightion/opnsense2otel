// Package metricsnap passively captures metrics produced by real gathers.
package metricsnap

import (
	"encoding/binary"
	"sort"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// Capture is the latest recorded view and its gather-error history.
type Capture struct {
	Families    []*dto.MetricFamily
	At          time.Time
	Partial     bool
	LastErrorAt time.Time
	ErrorCount  uint64
}

type laneCapture struct {
	families []*dto.MetricFamily
	at       time.Time
	version  uint64
	partial  bool
}

const defaultLane = ""

// Recorder records complete views without initiating gathers of its own.
type Recorder struct {
	mu sync.RWMutex
	// effects serializes publication and its budget effects, while allowing
	// Capture and new gather starts to proceed during an Observed callback.
	effects     sync.Mutex
	next        uint64
	started     bool
	configured  bool
	members     map[string]struct{}
	attempts    map[string]uint64
	staged      map[string]laneCapture
	cutoff      uint64
	view        laneCapture
	mode        string // empty, full, or split; provisional views leave mode empty
	lastErrorAt time.Time
	errorCount  uint64
	budget      SeriesBudget
	budgetState budgetState
}

// New returns an empty Recorder.
func New() *Recorder {
	return &Recorder{attempts: make(map[string]uint64), staged: make(map[string]laneCapture)}
}

// ConfigureSplitLanes declares all active split readers once, before any
// gather starts. Even an empty reader must be declared. No call means no split.
// Duplicate, empty or late declarations are programming errors.
func (r *Recorder) ConfigureSplitLanes(lanes ...string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.configured || r.started {
		panic("metricsnap: split membership already fixed")
	}
	r.configured = true
	r.members = make(map[string]struct{}, len(lanes))
	for _, lane := range lanes {
		if lane == defaultLane {
			panic("metricsnap: empty split lane")
		}
		if _, exists := r.members[lane]; exists {
			panic("metricsnap: duplicate split lane")
		}
		r.members[lane] = struct{}{}
	}
}

type teed struct {
	r     *Recorder
	lane  string
	split bool
	inner prometheus.Gatherer
}

// Tee wraps a full-view gatherer, preserving its result and error.
func (r *Recorder) Tee(inner prometheus.Gatherer) prometheus.Gatherer {
	return teed{r: r, inner: inner}
}

// TeeLane wraps a declared split reader. Wrapping does not declare membership.
func (r *Recorder) TeeLane(lane string, inner prometheus.Gatherer) prometheus.Gatherer {
	return teed{r: r, lane: lane, split: true, inner: inner}
}

func (t teed) Gather() ([]*dto.MetricFamily, error) {
	r := t.r
	r.mu.Lock()
	r.started = true
	r.next++
	version, at := r.next, time.Now()
	r.mu.Unlock()
	mfs, err := t.inner.Gather()

	r.effects.Lock()
	defer r.effects.Unlock()
	r.mu.Lock()
	if t.split {
		if _, ok := r.members[t.lane]; !ok {
			r.mu.Unlock()
			return mfs, err
		}
	}
	if err != nil {
		r.errorCount++
		r.lastErrorAt = time.Now()
	}
	if version <= r.attempts[t.lane] {
		r.mu.Unlock()
		return mfs, err
	}
	r.attempts[t.lane] = version
	capture := laneCapture{families: mfs, at: at, version: version, partial: err != nil}
	var totals []int
	if len(r.members) == 0 {
		if len(mfs) != 0 {
			r.view = capture
		}
		totals = append(totals, totalSeriesOf(r.view.families))
	} else if err != nil {
		if t.split && r.mode != "split" {
			delete(r.staged, t.lane)
		}
		if r.mode == "" && len(mfs) != 0 && version > r.view.version {
			r.view = capture
		}
	} else if !t.split {
		if r.mode == "" || version > r.view.version {
			capture.families = orderedFamilies(mfs)
			r.view, r.mode, r.cutoff = capture, "full", version
			for lane, stage := range r.staged {
				if stage.version <= version {
					delete(r.staged, lane)
				}
			}
			totals = append(totals, totalSeriesOf(mfs))
			if r.completeStages() {
				r.publishSplit()
				totals = append(totals, totalSeriesOf(r.view.families))
			}
		}
	} else {
		if r.mode == "split" || version > r.cutoff {
			r.staged[t.lane] = capture
			if r.mode == "split" || r.completeStages() {
				r.publishSplit()
				totals = append(totals, totalSeriesOf(r.view.families))
			} else if r.mode == "" && version > r.view.version {
				capture.partial = true
				r.view = capture
			}
		}
	}
	cfg := r.budget
	r.mu.Unlock()
	for _, total := range totals {
		r.observeSeries(total, cfg)
	}
	return mfs, err
}

func (r *Recorder) completeStages() bool {
	for lane := range r.members {
		if _, ok := r.staged[lane]; !ok {
			return false
		}
	}
	return len(r.members) != 0
}

// labelKey is a lossless canonical representation of all label pairs.
func labelKey(m *dto.Metric) string {
	labels := append([]*dto.LabelPair(nil), m.GetLabel()...)
	sort.Slice(labels, func(i, j int) bool {
		if labels[i].GetName() != labels[j].GetName() {
			return labels[i].GetName() < labels[j].GetName()
		}
		return labels[i].GetValue() < labels[j].GetValue()
	})
	var key []byte
	for _, label := range labels {
		for _, part := range []string{label.GetName(), label.GetValue()} {
			key = binary.AppendUvarint(key, uint64(len(part)))
			key = append(key, part...)
		}
	}
	return string(key)
}

// orderedFamilies copies containers and slices, never the gatherer's DTOs.
func orderedFamilies(in []*dto.MetricFamily) []*dto.MetricFamily {
	out := make([]*dto.MetricFamily, 0, len(in))
	for _, source := range in {
		mf := &dto.MetricFamily{Name: source.Name, Help: source.Help, Type: source.Type, Unit: source.Unit, Metric: append([]*dto.Metric(nil), source.Metric...)}
		sort.SliceStable(mf.Metric, func(i, j int) bool { return labelKey(mf.Metric[i]) < labelKey(mf.Metric[j]) })
		out = append(out, mf)
	}
	sort.SliceStable(out, func(i, j int) bool { return out[i].GetName() < out[j].GetName() })
	return out
}

func (r *Recorder) publishSplit() {
	type sample struct {
		metric  *dto.Metric
		version uint64
	}
	type family struct {
		source  *dto.MetricFamily
		version uint64
		samples map[string]sample
	}
	families := make(map[string]*family)
	var oldest time.Time
	var newest uint64
	for _, lane := range r.staged {
		if oldest.IsZero() || lane.at.Before(oldest) {
			oldest = lane.at
		}
		if lane.version > newest {
			newest = lane.version
		}
		for _, mf := range lane.families {
			f := families[mf.GetName()]
			if f == nil {
				f = &family{samples: make(map[string]sample)}
				families[mf.GetName()] = f
			}
			if f.source == nil || lane.version > f.version {
				f.source, f.version = mf, lane.version
			}
			for _, metric := range mf.GetMetric() {
				key := labelKey(metric)
				if prev, ok := f.samples[key]; !ok || lane.version > prev.version {
					f.samples[key] = sample{metric, lane.version}
				}
			}
		}
	}
	names := make([]string, 0, len(families))
	for name := range families {
		names = append(names, name)
	}
	sort.Strings(names)
	out := make([]*dto.MetricFamily, 0, len(names))
	for _, name := range names {
		f := families[name]
		// Construct a fresh container; source DTOs and their slices remain untouched.
		mf := &dto.MetricFamily{Name: f.source.Name, Help: f.source.Help, Type: f.source.Type, Unit: f.source.Unit}
		keys := make([]string, 0, len(f.samples))
		for key := range f.samples {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		for _, key := range keys {
			mf.Metric = append(mf.Metric, f.samples[key].metric)
		}
		out = append(out, mf)
	}
	r.view = laneCapture{families: out, at: oldest, version: newest}
	r.mode = "split"
}

// Snapshot returns the recorded family slice and capture time.
func (r *Recorder) Snapshot() ([]*dto.MetricFamily, time.Time) {
	c := r.Capture()
	return c.Families, c.At
}

// Capture returns the recorded view and cumulative error diagnostics.
func (r *Recorder) Capture() Capture {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return Capture{Families: append([]*dto.MetricFamily(nil), r.view.families...), At: r.view.at,
		Partial: r.view.partial, LastErrorAt: r.lastErrorAt, ErrorCount: r.errorCount}
}
