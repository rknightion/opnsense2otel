package metricsnap

import (
	"context"
	"log/slog"
	"sync"
	"testing"
	"time"

	dto "github.com/prometheus/client_model/go"
)

// countingHandler is a minimal slog.Handler that just counts records handled,
// so tests can assert "how many warnings fired" without parsing text output.
type countingHandler struct {
	mu      sync.Mutex
	records []slog.Record
}

func (h *countingHandler) Enabled(context.Context, slog.Level) bool { return true }

func (h *countingHandler) Handle(_ context.Context, r slog.Record) error {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.records = append(h.records, r)
	return nil
}

func (h *countingHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *countingHandler) WithGroup(string) slog.Handler      { return h }

func (h *countingHandler) count() int {
	h.mu.Lock()
	defer h.mu.Unlock()
	return len(h.records)
}

// familyWithSeries builds a metric family carrying exactly n series (unlike the
// zero-series `family` helper used by the lane-merge tests), so budget tests can
// drive a real total-series count across the budget threshold.
func familyWithSeries(name string, n int) *dto.MetricFamily {
	metrics := make([]*dto.Metric, n)
	for i := range metrics {
		metrics[i] = &dto.Metric{}
	}
	fname := name
	return &dto.MetricFamily{Name: &fname, Metric: metrics}
}

// clockStub is an injectable clock for the budget tests, so the hourly
// repeat window is driven deterministically instead of a wall-clock sleep.
type clockStub struct {
	mu  sync.Mutex
	cur time.Time
}

func newClockStub(start time.Time) *clockStub { return &clockStub{cur: start} }

func (c *clockStub) now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.cur
}

func (c *clockStub) advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cur = c.cur.Add(d)
}

// Publications and budget side effects cannot overtake a blocked callback;
// provisional and failed split results must not falsely report recovery.
func TestRecorder_SplitBudgetPublicationOrder(t *testing.T) {
	r := New()
	r.ConfigureSplitLanes("base", "fast")
	entered, release := make(chan struct{}), make(chan struct{})
	overtook := make(chan struct{}, 1)
	var releaseOnce sync.Once
	t.Cleanup(func() { releaseOnce.Do(func() { close(release) }) })
	var mu sync.Mutex
	var totals []int
	handler := &countingHandler{}
	r.ConfigureSeriesBudget(SeriesBudget{Total: 2, Logger: slog.New(handler), Observed: func(n int) {
		if n == 2 {
			close(entered)
			select {
			case <-release:
			case <-time.After(5 * time.Second):
				return
			}
		} else {
			select {
			case overtook <- struct{}{}:
			default:
			}
		}
		mu.Lock()
		totals = append(totals, n)
		mu.Unlock()
	}})
	first := make(chan struct{})
	go func() { _, _ = r.Tee(gathererOf(familyWithSeries("full", 2))).Gather(); close(first) }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("callback not entered")
	}
	// A second full gather is itself an eligible complete publication, unlike
	// an isolated split member. It must not overtake the blocked first effect.
	secondInner := gated(familyWithSeries("newfull", 1))
	second := startGated(t, r.Tee(secondInner), secondInner)
	close(secondInner.release)
	// Capture remains readable while the callback is blocked.
	if c := r.Capture(); len(c.Families) != 1 || c.Families[0].GetName() != "full" {
		t.Fatalf("capture while callback blocked: %+v", c)
	}
	select {
	case <-overtook:
		t.Fatal("later complete publication effect overtook blocked first effect")
	case <-time.After(200 * time.Millisecond):
	}
	releaseOnce.Do(func() { close(release) })
	select {
	case <-first:
	case <-time.After(5 * time.Second):
		t.Fatal("first gather stuck")
	}
	select {
	case <-second:
	case <-time.After(5 * time.Second):
		t.Fatal("second gather stuck")
	}
	_, _ = r.TeeLane("base", gathererOf(family("base"))).Gather()
	_, _ = r.TeeLane("fast", errGathererOf()).Gather()
	mu.Lock()
	got := append([]int(nil), totals...)
	mu.Unlock()
	if len(got) != 2 || got[0] != 2 || got[1] != 1 || handler.count() != 2 {
		t.Fatalf("ordered full effects or incomplete handoff: totals %v, logs %d", got, handler.count())
	}
	handler.mu.Lock()
	levels := []slog.Level{handler.records[0].Level, handler.records[1].Level}
	handler.mu.Unlock()
	if levels[0] != slog.LevelWarn || levels[1] != slog.LevelInfo {
		t.Fatalf("budget transition order: %v", levels)
	}
	_, _ = r.TeeLane("fast", gathererOf(labelledFamily(1, "collector", "fast"))).Gather()
	mu.Lock()
	got = append([]int(nil), totals...)
	mu.Unlock()
	if len(got) != 3 || got[2] != 1 || handler.count() != 2 {
		t.Fatalf("complete split budget: totals %v, logs %d", got, handler.count())
	}
}

// TestRecorder_SeriesBudgetRateLimitsRepeatedOverBudgetLogs verifies that
// repeated gathers over budget do not flood the log.
func TestRecorder_SeriesBudgetRateLimitsRepeatedOverBudgetLogs(t *testing.T) {
	h := &countingHandler{}
	logger := slog.New(h)
	clock := newClockStub(time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC))

	r := New()
	r.ConfigureSeriesBudget(SeriesBudget{
		Total:  10,
		Logger: logger,
		Now:    clock.now,
	})

	over := gathererOf(familyWithSeries("opnsense_over", 20))
	teed := r.Tee(over)

	// 50 gathers at the identical simulated instant: only the FIRST is a
	// transition into over-budget. Without the rate-limit guard this would be
	// 50 log lines, not 1.
	for i := 0; i < 50; i++ {
		if _, err := teed.Gather(); err != nil {
			t.Fatalf("gather %d: %v", i, err)
		}
	}
	if got := h.count(); got != 1 {
		t.Fatalf("after 50 over-budget gathers at the same instant, want 1 log (transition-in only), got %d", got)
	}

	// Advance 30 minutes (< the 1h repeat window) and gather again repeatedly:
	// still no new log.
	clock.advance(30 * time.Minute)
	for i := 0; i < 20; i++ {
		if _, err := teed.Gather(); err != nil {
			t.Fatalf("gather: %v", err)
		}
	}
	if got := h.count(); got != 1 {
		t.Fatalf("after 30 more minutes still over budget, want log count unchanged at 1, got %d", got)
	}

	// Advance past the 1h window since the last log (30m + 31m = 61m) and
	// gather again: exactly one more log (the hourly repeat), not one per call.
	clock.advance(31 * time.Minute)
	for i := 0; i < 20; i++ {
		if _, err := teed.Gather(); err != nil {
			t.Fatalf("gather: %v", err)
		}
	}
	if got := h.count(); got != 2 {
		t.Fatalf("after crossing the 1h repeat window, want exactly 2 total logs, got %d", got)
	}

	// Drop back under budget: exactly one more log (transition back), however
	// many times it is subsequently gathered under budget.
	under := gathererOf(familyWithSeries("opnsense_under", 3))
	teedUnder := r.Tee(under)
	for i := 0; i < 20; i++ {
		if _, err := teedUnder.Gather(); err != nil {
			t.Fatalf("gather: %v", err)
		}
	}
	if got := h.count(); got != 3 {
		t.Fatalf("after transitioning back under budget (repeatedly), want exactly 3 total logs, got %d", got)
	}
}

// TestRecorder_SeriesBudgetDisabledNeverLogs pins Total == 0 as "disabled": no
// log, ever, regardless of how far over any real series count would be.
func TestRecorder_SeriesBudgetDisabledNeverLogs(t *testing.T) {
	h := &countingHandler{}
	r := New()
	r.ConfigureSeriesBudget(SeriesBudget{Total: 0, Logger: slog.New(h)})

	teed := r.Tee(gathererOf(familyWithSeries("opnsense_huge", 1000)))
	for i := 0; i < 10; i++ {
		if _, err := teed.Gather(); err != nil {
			t.Fatalf("gather: %v", err)
		}
	}
	if got := h.count(); got != 0 {
		t.Fatalf("budget disabled (Total=0): want 0 logs, got %d", got)
	}
}

// TestRecorder_SeriesBudgetUnderBudgetNeverLogs pins the common steady state:
// staying under budget produces no log at all.
func TestRecorder_SeriesBudgetUnderBudgetNeverLogs(t *testing.T) {
	h := &countingHandler{}
	r := New()
	r.ConfigureSeriesBudget(SeriesBudget{Total: 100, Logger: slog.New(h)})

	teed := r.Tee(gathererOf(familyWithSeries("opnsense_small", 5)))
	for i := 0; i < 10; i++ {
		if _, err := teed.Gather(); err != nil {
			t.Fatalf("gather: %v", err)
		}
	}
	if got := h.count(); got != 0 {
		t.Fatalf("staying under budget: want 0 logs, got %d", got)
	}
}

// TestRecorder_SeriesBudgetObservedCallback pins that Observed fires with the
// current total on every real Gather, independent of whether a budget is even
// configured — it feeds the opnsense_exporter_series self-metric.
func TestRecorder_SeriesBudgetObservedCallback(t *testing.T) {
	var mu sync.Mutex
	var last int
	var calls int
	r := New()
	r.ConfigureSeriesBudget(SeriesBudget{
		Observed: func(total int) {
			mu.Lock()
			defer mu.Unlock()
			last = total
			calls++
		},
	})

	teed := r.Tee(gathererOf(familyWithSeries("opnsense_a", 4), familyWithSeries("opnsense_b", 3)))
	if _, err := teed.Gather(); err != nil {
		t.Fatalf("gather: %v", err)
	}

	mu.Lock()
	defer mu.Unlock()
	if calls != 1 {
		t.Fatalf("want Observed called once, got %d", calls)
	}
	if last != 7 {
		t.Fatalf("want Observed(7), got Observed(%d)", last)
	}
}
