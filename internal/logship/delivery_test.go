package logship

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	sdklog "go.opentelemetry.io/otel/sdk/log"

	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

// flakySink fails its first failN Emit calls (or every call, when always is set) then
// succeeds. Emit is driven serially by the single emitter; the mutex only guards the
// call counter for a test goroutine that reads it.
type flakySink struct {
	mu     sync.Mutex
	calls  int
	failN  int
	always bool
}

func (s *flakySink) Emit(_ context.Context, batch []Entry) SinkResult {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.calls++
	if s.always || s.calls <= s.failN {
		return retryResult(batch, errors.New("export refused"))
	}
	return ackedResult(batch)
}
func (s *flakySink) Shutdown(context.Context) error { return nil }
func (s *flakySink) callCount() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.calls
}

// blockingSink blocks inside Emit until release is closed, signalling started once.
type blockingSink struct {
	started chan struct{}
	release chan struct{}
}

func (s *blockingSink) Emit(_ context.Context, batch []Entry) SinkResult {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	return ackedResult(batch)
}
func (s *blockingSink) Shutdown(context.Context) error { return nil }

// erroringExporter fails every Export, to prove the sink surfaces the failure as a
// returned error rather than swallowing it.
type erroringExporter struct{}

func (erroringExporter) Export(context.Context, []sdklog.Record) error {
	return errors.New("endpoint unavailable")
}
func (erroringExporter) Shutdown(context.Context) error   { return nil }
func (erroringExporter) ForceFlush(context.Context) error { return nil }

func newDeliveryPipeline(sink Sink, reg prometheus.Registerer) (*pipeline, context.CancelFunc) {
	ctx, cancel := context.WithCancel(context.Background())
	p := &pipeline{
		sink:    sink,
		cfg:     &options.LogsConfig{BatchMax: 100, BufferSize: 100},
		log:     slog.New(slog.NewTextHandler(os.Stderr, nil)),
		ctx:     ctx,
		cancel:  cancel,
		limiter: NewLogLimiter(errorLogInterval, errorLogMaxKeys),
	}
	p.queue = newBoundedQueue(100, func(Entry) {})
	p.metrics = newMetrics(reg, queueBounds{capacity: 100, length: func() float64 { return 0 }}, sourceNames{all: []string{"firewall"}})
	return p, cancel
}

// A transient export failure must NOT lose records or count them as shipped: the batch
// is retried in memory until the endpoint recovers, then shipped exactly once (#290).
func TestShipBatch_RetriesTransientFailureThenShips(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &flakySink{failN: 2}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()

	p.shipBatch([]Entry{
		{Source: "firewall", Record: Record{Body: "a"}},
		{Source: "firewall", Record: Record{Body: "b"}},
	})

	s := gatherSeries(t, reg)
	if got := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 2 {
		t.Fatalf("shipped=%v, want 2 (both records delivered after retry)", got)
	}
	if got := s[`opnsense_exporter_logs_ship_errors_total`]; got != 2 {
		t.Fatalf("ship_errors=%v, want 2 (two transient failures observed)", got)
	}
	if got := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 0 {
		t.Fatalf("ship_failed=%v, want 0 (nothing permanently lost)", got)
	}
	if c := sink.callCount(); c != 3 {
		t.Fatalf("sink Emit called %d times, want 3 (2 fail + 1 success)", c)
	}
}

// When the pipeline is shutting down and the endpoint is still failing, an undeliverable
// batch is abandoned and every record counted logs_dropped_total{reason="ship_failed"} —
// never silently skipped and never counted as shipped (#290).
func TestShipBatch_AbandonsUndeliverableBatchOnShutdown(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &flakySink{always: true}
	p, cancel := newDeliveryPipeline(sink, reg)
	cancel() // shutdown already in progress

	done := make(chan struct{})
	go func() {
		p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a"}}})
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("shipBatch did not return promptly after shutdown; the drain would hang")
	}

	s := gatherSeries(t, reg)
	if got := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 1 {
		t.Fatalf("ship_failed=%v, want 1 (the abandoned record is counted)", got)
	}
	if got := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 0 {
		t.Fatalf("shipped=%v, want 0 (it was never delivered)", got)
	}
	if got := s[`opnsense_exporter_logs_ship_errors_total`]; got < 1 {
		t.Fatalf("ship_errors=%v, want >=1", got)
	}
}

// stop must not report a clean flush when the emitter cannot finish before the shutdown
// deadline: it returns an error rather than letting cursors imply delivery (#290).
func TestStop_ReturnsErrorWhenDrainExceedsDeadline(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &blockingSink{started: make(chan struct{}, 1), release: make(chan struct{})}
	p, _ := newDeliveryPipeline(sink, reg)

	p.emitterWG.Add(1)
	go p.runEmitter()
	p.queue.push(Entry{Source: "firewall", Record: Record{Body: "a"}})
	<-sink.started // the emitter is now blocked inside Emit

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	err := p.stop(ctx)
	if err == nil {
		t.Fatal("stop reported a clean shutdown while the drain was still blocked")
	}

	close(sink.release) // let the emitter finish so the goroutine does not leak
}

// --- #394: stage-correct freshness ----------------------------------------
//
// The old logs_last_event_timestamp_seconds was advanced right after queue ADMISSION
// using the record's SOURCE-ORIGIN timestamp, but documented and alerted as "the last
// event shipped". Three ways that lied: it moved before the sink acknowledged
// anything, it regressed on out-of-order input, and — because an enabled syslog
// receiver listens on all interfaces with an empty peer allowlist by default — any
// admitted sender could push it years into the future and silently suppress the
// stalled-source alert. It is REPLACED (not deprecated) by two exporter-clock gauges.

// Export freshness must advance ONLY when the sink acknowledges. A batch retried twice
// then delivered advances it exactly once, at acknowledgement.
func TestShipBatch_ExportFreshnessAdvancesOnlyOnAcknowledgement(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &flakySink{failN: 2}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()

	before := float64(time.Now().Unix())
	p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a"}}})

	s := gatherSeries(t, reg)
	got, ok := s[`opnsense_exporter_logs_last_exported_timestamp_seconds{source="firewall"}`]
	if !ok {
		t.Fatal("logs_last_exported_timestamp_seconds is absent after an acknowledged delivery")
	}
	if got < before {
		t.Fatalf("export freshness = %v, want >= %v (the acknowledgement time)", got, before)
	}
}

// A batch the sink never acknowledges must NOT advance export freshness — that is the
// whole point of splitting the gauge. Before #394 the single gauge had already moved at
// admission, so a wedged sink looked healthy.
func TestShipBatch_ExportFreshnessNeverAdvancesForAbandonedBatch(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &flakySink{always: true}
	p, cancel := newDeliveryPipeline(sink, reg)
	cancel() // shutdown already in progress

	p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a"}}})

	s := gatherSeries(t, reg)
	if v, ok := s[`opnsense_exporter_logs_last_exported_timestamp_seconds{source="firewall"}`]; ok {
		t.Fatalf("export freshness = %v for a batch that was never delivered; want the series absent", v)
	}
}

// The ambiguous gauge is REMOVED outright (owner decision on #449): no alias, no
// deprecation window. A rebuilt alias would leave the dashboards ambiguous for another
// release cycle, and the run is cutting a major anyway.
func TestMetrics_LastEventTimestampGaugeIsGone(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &flakySink{}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()
	p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a", Timestamp: time.Now()}}})

	for key := range gatherSeries(t, reg) {
		if strings.HasPrefix(key, "opnsense_exporter_logs_last_event_timestamp_seconds") {
			t.Fatalf("%s still exists; it was removed in favour of the last_received/last_exported pair", key)
		}
	}
}

// The OTLP sink must surface a failed export from Emit, not swallow it the way the old
// fire-and-forget Logger.Emit path did (#290) — that signal is what lets the pipeline
// retry instead of counting an undelivered batch as shipped. Since #392 it surfaces as
// the batch coming back in Retry rather than as a bare error.
func TestOTLPSink_EmitReturnsErrorWhenExportFails(t *testing.T) {
	s := newTestSink(erroringExporter{})
	batch := []Entry{{Source: "syslog", Record: Record{
		Body:       "denied",
		Attributes: map[string]string{AttrSubsystem: "firewall"},
	}}}
	res := s.Emit(context.Background(), batch)
	if len(res.Acked) != 0 {
		t.Fatal("Emit acknowledged records despite the exporter failing every Export")
	}
	if len(res.Retry) != 1 || res.Err == nil {
		t.Fatalf("Emit returned %+v; want the batch back for retry with the underlying error", res)
	}
	_ = s.Shutdown(context.Background())
}

// --- #392: the pipeline's side of the resource-aware contract ---------------

// partitionedSink acknowledges some entries, terminally rejects others and asks for the
// rest back, in one call — the shape a real OTLP sink produces when one resource
// partition succeeds, a second is refused and a third fails transiently.
type partitionedSink struct {
	mu     sync.Mutex
	calls  int
	result func(batch []Entry) SinkResult
	seen   [][]string
}

func (s *partitionedSink) Emit(_ context.Context, batch []Entry) SinkResult {
	s.mu.Lock()
	s.calls++
	got := make([]string, 0, len(batch))
	for _, e := range batch {
		got = append(got, e.Record.Body)
	}
	s.seen = append(s.seen, got)
	fn := s.result
	s.mu.Unlock()
	return fn(batch)
}

func (s *partitionedSink) Shutdown(context.Context) error { return nil }

func (s *partitionedSink) attempts() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.seen...)
}

// The pipeline must count each record exactly once and re-send only what came back in
// Retry. An acknowledged record being resent is the duplication bug; an acknowledged
// record counted as dropped is the undercounting bug the issue warns against trading
// it for.
func TestShipBatch_RetriesOnlyTheUnacknowledgedRemainder(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &partitionedSink{}
	sink.result = func(batch []Entry) SinkResult {
		sink.mu.Lock()
		first := sink.calls == 1
		sink.mu.Unlock()
		if !first {
			return ackedResult(batch)
		}
		// a=acked, b=terminally rejected, c=retryable
		return SinkResult{
			Acked:    []Entry{batch[0]},
			Rejected: []Entry{batch[1]},
			Retry:    []Entry{batch[2]},
			Err:      errors.New("mixed partition outcome"),
		}
	}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()

	p.shipBatch([]Entry{
		{Source: "firewall", Record: Record{Body: "a"}},
		{Source: "firewall", Record: Record{Body: "b"}},
		{Source: "firewall", Record: Record{Body: "c"}},
	})

	got := sink.attempts()
	if len(got) != 2 {
		t.Fatalf("sink called %d times, want 2 (initial + one retry): %v", len(got), got)
	}
	if !slices.Equal(got[1], []string{"c"}) {
		t.Fatalf("retry carried %v, want only the unacknowledged remainder [c]", got[1])
	}

	s := gatherSeries(t, reg)
	if v := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; v != 2 {
		t.Fatalf("shipped=%v, want 2 (a acknowledged on attempt 1, c on attempt 2)", v)
	}
	if v := s[`opnsense_exporter_logs_dropped_total{reason="rejected",source="firewall"}`]; v != 1 {
		t.Fatalf("dropped{rejected}=%v, want 1 (b was terminally refused)", v)
	}
	// The three families must reconcile: every record is either shipped or dropped,
	// exactly once, and an acknowledged record is never counted as dropped.
	var droppedTotal float64
	for _, reason := range []string{"overflow", "record_too_large", "rejected", "ship_failed", "ship_failed_permanent"} {
		droppedTotal += s[`opnsense_exporter_logs_dropped_total{reason="`+reason+`",source="firewall"}`]
	}
	if total := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`] + droppedTotal; total != 3 {
		t.Fatalf("shipped+dropped=%v, want exactly 3 — the logical batch must reconcile", total)
	}
	if v := s[`opnsense_exporter_logs_ship_errors_total`]; v != 1 {
		t.Fatalf("ship_errors=%v, want 1 (attempt 1 did not fully deliver; attempt 2 did)", v)
	}
}

// A terminal rejection must cost ONE wire attempt, not --logs.ship-max-attempts of
// them, and must be counted under its own reason so an operator can tell "the endpoint
// refuses this payload" from "the endpoint is down".
func TestShipBatch_TerminalRejectionIsNotRetried(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &partitionedSink{result: func(batch []Entry) SinkResult {
		return SinkResult{Rejected: batch, Err: errors.New("400 bad request")}
	}}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()
	p.cfg.ShipMaxAttempts = 10

	p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a"}}})

	if n := len(sink.attempts()); n != 1 {
		t.Fatalf("sink called %d times for a permanent refusal, want exactly 1", n)
	}
	s := gatherSeries(t, reg)
	if v := s[`opnsense_exporter_logs_dropped_total{reason="rejected",source="firewall"}`]; v != 1 {
		t.Fatalf("dropped{rejected}=%v, want 1", v)
	}
	if v := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed_permanent",source="firewall"}`]; v != 0 {
		t.Fatalf("dropped{ship_failed_permanent}=%v, want 0 — this was a refusal, not exhausted retries", v)
	}
	if v := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; v != 0 {
		t.Fatalf("shipped=%v, want 0 — a refused record was never delivered", v)
	}
	if _, ok := s[`opnsense_exporter_logs_last_exported_timestamp_seconds{source="firewall"}`]; ok {
		t.Fatal("export freshness advanced for a terminally rejected batch; it must track acknowledgement only")
	}
}

// A batch that is fully acknowledged in one call must not touch the error counter — the
// mixed-outcome bookkeeping must not start counting successes as failures.
func TestShipBatch_CleanDeliveryCountsNoShipError(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &partitionedSink{result: ackedResult}
	p, cancel := newDeliveryPipeline(sink, reg)
	defer cancel()

	p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "a"}}})

	s := gatherSeries(t, reg)
	if v := s[`opnsense_exporter_logs_ship_errors_total`]; v != 0 {
		t.Fatalf("ship_errors=%v, want 0 for a clean delivery", v)
	}
	if v := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; v != 1 {
		t.Fatalf("shipped=%v, want 1", v)
	}
}

// The elapsed budget must cap the whole batch, including successful partial
// progress, and count only its undelivered remainder when it expires.
func TestShipBatch_ElapsedBudgetSurvivesPartialAcknowledgement(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		sink := &partitionedSink{result: func(batch []Entry) SinkResult {
			time.Sleep(500 * time.Millisecond)
			return ackedResult(batch)
		}}
		p, cancel := newDeliveryPipeline(sink, reg)
		defer cancel()
		p.cfg.ShipMaxElapsed = time.Second
		p.shipChunk = 1
		start := time.Now()
		p.shipBatch([]Entry{{Source: "firewall"}, {Source: "firewall"}, {Source: "firewall"}, {Source: "firewall"}})
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("elapsed=%v: deadline was reset by progress", elapsed)
		}
		series := gatherSeries(t, reg)
		if got := series[`opnsense_exporter_logs_dropped_total{reason="ship_failed_permanent",source="firewall"}`]; got != 2 {
			t.Fatalf("expired remainder=%v, want2", got)
		}
	})
}

func TestShipBatch_ElapsedBudgetExpiresRetryAndCountsLoss(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		sink := &flakySink{always: true}
		p, cancel := newDeliveryPipeline(sink, reg)
		defer cancel()
		p.cfg.ShipMaxElapsed = time.Second
		// Bound the old implementation too, so the red test terminates.
		p.cfg.ShipMaxAttempts = 20
		start := time.Now()
		p.shipBatch([]Entry{{Source: "firewall"}, {Source: "firewall"}})
		if elapsed := time.Since(start); elapsed != time.Second {
			t.Fatalf("elapsed=%v, want1s", elapsed)
		}
		series := gatherSeries(t, reg)
		if got := series[`opnsense_exporter_logs_dropped_total{reason="ship_failed_permanent",source="firewall"}`]; got != 2 {
			t.Fatalf("expired=%v, want2", got)
		}
	})
}

func TestPipeline_WANOutageEightMinutesThenRecovery(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		start := time.Now()
		failures := 0
		var delivered []string
		sink := &partitionedSink{result: func(batch []Entry) SinkResult {
			if time.Since(start) < 8*time.Minute {
				failures++
				return retryResult(batch, errors.New("WAN unreachable"))
			}
			for _, e := range batch {
				delivered = append(delivered, e.Record.Body)
			}
			return ackedResult(batch)
		}}
		p, cancel := newDeliveryPipeline(sink, reg)
		defer cancel()
		// These are the public defaults. Exercise the real queue/emitter path.
		p.cfg.ShipMaxElapsed = 15 * time.Minute
		p.cfg.ShipMaxAttempts = 0
		p.cfg.BatchMax = 1
		p.queue.push(Entry{Source: "firewall", Record: Record{Body: "first"}})
		p.queue.push(Entry{Source: "firewall", Record: Record{Body: "second"}})
		p.queue.close()
		p.emitterWG.Add(1)
		p.runEmitter()
		if failures <= 10 {
			t.Fatalf("only%d failures, need to exceed the former attempt cap", failures)
		}
		if !slices.Equal(delivered, []string{"first", "second"}) {
			t.Fatalf("delivered=%v", delivered)
		}
		series := gatherSeries(t, reg)
		if got := series[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 2 {
			t.Fatalf("shipped=%v, want2", got)
		}
		if got := series[`opnsense_exporter_logs_dropped_total{reason="ship_failed_permanent",source="firewall"}`]; got != 0 {
			t.Fatalf("dropped=%v, want0", got)
		}
	})
}

// Real HTTP I/O proves the remaining budget reaches the SDK, rather than just
// limiting sleeps between otherwise unbounded export attempts.
func TestShipBatch_HTTPAttemptBoundedByElapsedAndShutdown(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		name := "elapsed"
		if shutdown {
			name = "shutdown"
		}
		t.Run(name, func(t *testing.T) {
			started := make(chan struct{}, 1)
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				select {
				case started <- struct{}{}:
				default:
				}
				select {
				case <-r.Context().Done():
					return
				case <-time.After(300 * time.Millisecond):
				}
				w.Header().Set("Content-Type", "application/x-protobuf")
				w.WriteHeader(http.StatusOK)
			}))
			defer srv.Close()
			reg := prometheus.NewRegistry()
			p, cancel := newDeliveryPipeline(newSinkOverEndpoint(t, "http/protobuf", srv.URL), reg)
			defer cancel()
			p.cfg.ShipMaxElapsed = 50 * time.Millisecond
			if shutdown {
				p.cfg.ShipMaxElapsed = 15 * time.Minute
				go func() {
					select {
					case <-started:
						cancel()
					case <-time.After(time.Second):
						cancel()
					}
				}()
			}
			p.shipBatch([]Entry{{Source: "firewall", Record: Record{Body: "outage"}}})
			reason := dropReasonShipFailedPermanent
			if shutdown {
				reason = dropReasonShipFailed
			}
			series := gatherSeries(t, reg)
			if got := series[`opnsense_exporter_logs_dropped_total{reason="`+reason+`",source="firewall"}`]; got != 1 {
				t.Fatalf("dropped=%v, want1 (export must be cancelled before server acknowledges)", got)
			}
			if got := series[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 0 {
				t.Fatalf("shipped=%v,want0", got)
			}
		})
	}
}

func TestStop_HTTPDrainsFinalRecordsUsingShutdownContext(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	reg := prometheus.NewRegistry()
	p, _ := newDeliveryPipeline(newSinkOverEndpoint(t, "http/protobuf", srv.URL), reg)
	p.cfg.BatchMax = 1
	p.afterSourcesStopped = func() {
		// The final producer flush happens after the run context is cancelled.
		p.queue.push(Entry{Source: "firewall", Record: Record{Body: "final-one"}})
		p.queue.push(Entry{Source: "firewall", Record: Record{Body: "final-two"}})
	}
	p.emitterWG.Add(1)
	go p.runEmitter()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := p.stop(ctx); err != nil {
		t.Fatal(err)
	}
	if got := requests.Load(); got != 2 {
		t.Fatalf("HTTP requests=%d, want2 healthy final-flush deliveries", got)
	}
	series := gatherSeries(t, reg)
	if got := series[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 2 {
		t.Fatalf("shipped=%v,want2", got)
	}
	if got := series[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 0 {
		t.Fatalf("shutdown dropped=%v,want0", got)
	}
}

func TestStop_HTTPDrainStopsAtCallerDeadlineWithoutRetry(t *testing.T) {
	var requests atomic.Int64
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		select {
		case <-r.Context().Done():
			return
		case <-time.After(500 * time.Millisecond):
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	reg := prometheus.NewRegistry()
	p, _ := newDeliveryPipeline(newSinkOverEndpoint(t, "http/protobuf", srv.URL), reg)
	p.afterSourcesStopped = func() { p.queue.push(Entry{Source: "firewall", Record: Record{Body: "final"}}) }
	p.emitterWG.Add(1)
	go p.runEmitter()
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = p.stop(ctx)
	p.emitterWG.Wait()
	if elapsed := time.Since(start); elapsed >= 400*time.Millisecond {
		t.Fatalf("drain elapsed=%v: caller deadline did not bound export", elapsed)
	}
	if got := requests.Load(); got != 1 {
		t.Fatalf("HTTP requests=%d, want1 bounded final delivery attempt", got)
	}
	if got := gatherSeries(t, reg)[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 1 {
		t.Fatalf("shutdown dropped=%v,want1", got)
	}
}

func TestStop_DrainDoesNotRetryFailedAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &flakySink{failN: 1}
		reg := prometheus.NewRegistry()
		p, _ := newDeliveryPipeline(sink, reg)
		p.afterSourcesStopped = func() { p.queue.push(Entry{Source: "firewall"}) }
		p.emitterWG.Add(1)
		go p.runEmitter()
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.stop(ctx); err != nil {
			t.Fatal(err)
		}
		if got := sink.callCount(); got != 1 {
			t.Fatalf("delivery attempts=%d,want1 without shutdown retry", got)
		}
		if got := gatherSeries(t, reg)[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 1 {
			t.Fatalf("shutdown dropped=%v,want1", got)
		}
	})
}

// --- graceful-restart loss: in-flight and waiting batches at stop ---------
//
// A healthy destination must not lose the batch the emitter is holding when SIGTERM
// arrives. Before the fix, stop's producer cancellation also cancelled that batch's
// delivery context, so an export in progress, a retry backoff or a rate-limit pace
// was cut off and the whole batch was counted ship_failed although the endpoint was
// fine. The batch now gets its final attempt bounded by the stop caller's deadline.

// ctxCheckingSink holds Emit until release is closed, then acknowledges unless the
// delivery context was cancelled meanwhile, the way an HTTP export cancelled
// mid-request fails.
type ctxCheckingSink struct {
	started chan struct{}
	release chan struct{}
}

func (s *ctxCheckingSink) Emit(ctx context.Context, batch []Entry) SinkResult {
	select {
	case s.started <- struct{}{}:
	default:
	}
	<-s.release
	if err := ctx.Err(); err != nil {
		return retryResult(batch, err)
	}
	return ackedResult(batch)
}
func (s *ctxCheckingSink) Shutdown(context.Context) error { return nil }

func TestStop_InFlightExportCompletesUnderShutdownDeadline(t *testing.T) {
	reg := prometheus.NewRegistry()
	sink := &ctxCheckingSink{started: make(chan struct{}, 1), release: make(chan struct{})}
	p, _ := newDeliveryPipeline(sink, reg)
	p.emitterWG.Add(1)
	go p.runEmitter()
	p.queue.push(Entry{Source: "firewall", Record: Record{Body: "a"}})
	p.queue.push(Entry{Source: "firewall", Record: Record{Body: "b"}})
	<-sink.started // the emitter is inside Emit when shutdown begins

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	stopped := make(chan error, 1)
	go func() { stopped <- p.stop(ctx) }()
	for p.ctx.Err() == nil { // producer cancellation has happened
		time.Sleep(time.Millisecond)
	}
	close(sink.release) // the healthy endpoint answers after shutdown began

	if err := <-stopped; err != nil {
		t.Fatalf("stop: %v", err)
	}
	s := gatherSeries(t, reg)
	if got := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 0 {
		t.Fatalf("ship_failed=%v, want 0 (a healthy in-flight export must finish within the stop deadline)", got)
	}
	if got := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 2 {
		t.Fatalf("shipped=%v, want 2", got)
	}
}

// The stop deadline still bounds an export that was already in flight: a stuck
// endpoint cannot hold shutdown past it, and the abandoned batch is ship_failed.
func TestStop_HTTPInFlightExportStopsAtCallerDeadline(t *testing.T) {
	started := make(chan struct{}, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case started <- struct{}{}:
		default:
		}
		select {
		case <-r.Context().Done():
			return
		case <-time.After(2 * time.Second):
		}
		w.Header().Set("Content-Type", "application/x-protobuf")
		w.WriteHeader(http.StatusOK)
	}))
	defer srv.Close()
	reg := prometheus.NewRegistry()
	p, _ := newDeliveryPipeline(newSinkOverEndpoint(t, "http/protobuf", srv.URL), reg)
	p.emitterWG.Add(1)
	go p.runEmitter()
	p.queue.push(Entry{Source: "firewall", Record: Record{Body: "stuck"}})
	<-started

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_ = p.stop(ctx)
	p.emitterWG.Wait()
	if elapsed := time.Since(start); elapsed >= time.Second {
		t.Fatalf("stop+drain elapsed=%v: the caller deadline did not bound the in-flight export", elapsed)
	}
	if got := gatherSeries(t, reg)[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 1 {
		t.Fatalf("ship_failed=%v, want 1", got)
	}
}

// A batch waiting out a retry backoff when stop begins gets one final attempt
// instead of being abandoned the moment producers are cancelled.
func TestStop_BackoffWaitGetsFinalAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		sink := &flakySink{failN: 1}
		reg := prometheus.NewRegistry()
		p, _ := newDeliveryPipeline(sink, reg)
		p.emitterWG.Add(1)
		go p.runEmitter()
		p.queue.push(Entry{Source: "firewall", Record: Record{Body: "a"}})
		synctest.Wait() // first attempt failed; the emitter is parked in its backoff

		if got := sink.callCount(); got != 1 {
			t.Fatalf("attempts before stop=%d, want 1", got)
		}
		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.stop(ctx); err != nil {
			t.Fatal(err)
		}
		s := gatherSeries(t, reg)
		if got := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="firewall"}`]; got != 0 {
			t.Fatalf("ship_failed=%v, want 0 (the backoff must yield to a final attempt)", got)
		}
		if got := s[`opnsense_exporter_logs_shipped_total{source="firewall"}`]; got != 1 {
			t.Fatalf("shipped=%v, want 1", got)
		}
		if got := sink.callCount(); got != 2 {
			t.Fatalf("attempts=%d, want 2 (one before stop, one final)", got)
		}
	})
}

// The same for a rate-limit pace: the split chunks are attempted under the stop
// deadline rather than abandoned mid-pause.
func TestStop_PacingWaitGetsFinalAttempt(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		reg := prometheus.NewRegistry()
		sink := &rateLimitingSink{refuseAbove: 128}
		p, _ := ratePipeline(t, reg, sink)
		p.emitterWG.Add(1)
		go p.runEmitter()
		for _, e := range rateEntries(256) {
			p.queue.push(e)
		}
		synctest.Wait() // 256 refused; the emitter is pacing before the first 128 chunk

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()
		if err := p.stop(ctx); err != nil {
			t.Fatal(err)
		}
		s := gatherSeries(t, reg)
		if got := s[`opnsense_exporter_logs_dropped_total{reason="ship_failed",source="syslog"}`]; got != 0 {
			t.Fatalf("ship_failed=%v, want 0 (the pace must yield to final attempts)", got)
		}
		if got := s[`opnsense_exporter_logs_shipped_total{source="syslog"}`]; got != 256 {
			t.Fatalf("shipped=%v, want 256", got)
		}
	})
}
