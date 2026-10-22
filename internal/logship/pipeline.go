package logship

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/rknightion/opnsense2otel/v5/internal/options"
)

// stateFlushInterval bounds how often persisted cursors are rewritten while the
// pipeline runs. Writes are skipped when no cursor changed, and a final flush
// always runs on shutdown, so this only bounds the staleness window of the
// on-disk state — it does not tie persistence to any source's poll cadence.
const stateFlushInterval = 30 * time.Second

// errorLogInterval rate-limits repeated poll/ship error logging so a flapping
// endpoint cannot flood the logs.
const errorLogInterval = 30 * time.Second

// errorLogMaxKeys bounds the pipeline limiter's key set. Its keys are code-defined
// and few ("poll:"+source name, "oversize:"+source name, "ship", "ship_permanent"),
// so this is only a backstop and is never reached in practice.
const errorLogMaxKeys = 64

// splitDiagnosticLoggers separates the two logger roles the pipeline needs once
// self-log shipping is on. Without a self-log handler both roles collapse onto
// the caller's logger.
//
// pipelineLog drops to the wrapped stderr handler. Sink, retry, ingest-cap and
// shutdown diagnostics all describe the delivery path itself, so submitting one
// back into that path manufactures another record on every failed attempt while
// the endpoint is down.
//
// sourceLog keeps the forwarding handler. A source poll error describes the
// FIREWALL side, not the sink: it is the only place the reason behind a
// logs_poll_errors_total increment exists, and shipping it cannot make it recur.
// Routing it to stderr leaves an operator watching a counter move with the cause
// reachable only by reading the container's console (#OPN-0069).
func splitDiagnosticLoggers(logger *slog.Logger, selfLog *SelfLogHandler) (sourceLog, pipelineLog *slog.Logger) {
	if selfLog == nil {
		return logger, logger
	}
	return logger, selfLog.DiagnosticLogger()
}

// pipeline is the running log-shipping loop.
type pipeline struct {
	sink    Sink
	queue   *boundedQueue
	metrics *metrics
	cfg     *options.LogsConfig
	log     *slog.Logger
	// sourceLog carries diagnostics about a polled source. See
	// splitDiagnosticLoggers for why it is not log.
	sourceLog *slog.Logger
	selfLog   *SelfLogHandler

	sources     []Source
	pushSources []PushSource
	stateful    []StatefulSource
	stateFile   string

	ctx    context.Context
	cancel context.CancelFunc
	// stopCtx is published before producer cancellation. Producer cancellation does
	// NOT cancel delivery: every batch, including the one in flight or waiting out a
	// backoff or pace when stop begins, is bound to stopCtx from that point and gets
	// one delivery attempt per chunk before the stop caller's deadline. That deadline
	// is what interrupts a stuck export (see bindDeliveryToShutdown).
	stopMu  sync.Mutex
	stopCtx context.Context
	// afterSourcesStopped bridges producers outside the source registry into the
	// shutdown order. It runs after pollerWG reaches zero and before queue.close.
	afterSourcesStopped func()

	pollerWG  sync.WaitGroup
	emitterWG sync.WaitGroup

	stateMu       sync.Mutex
	lastPersisted []byte

	limiter *LogLimiter

	// shipChunk is the STICKY, adaptive per-attempt record cap learned from rate-limit
	// refusals (#663). 0 means "no cap, send the whole batch", which is the state a
	// healthy pipeline stays in forever. Only the single emitter goroutine touches it
	// (shipBatch is the sole writer, and runEmitter is the sole caller in production),
	// so it needs no lock.
	//
	// It is deliberately pipeline-scoped rather than batch-scoped. A sustained burst
	// above the tenant's budget produces a NEW oversized batch every drain, so a split
	// that reset each time would re-learn the limit — and pay one refused wire attempt
	// for it — on every single batch. Carrying it forward is what turns "burst above the
	// limit" into steady paced delivery rather than a refusal per batch.
	shipChunk int
}

// Start builds every registered source, the configured sink and the pipeline,
// then launches the poller/emitter/state goroutines. It performs no network I/O:
// the OTLP sink connects lazily on the first export, so a dead endpoint never
// blocks startup. It returns a stop function that drains the pipeline (stop
// pollers -> flush queue -> persist cursors -> flush sink), bounded by the stop
// context. When no source is enabled it logs a warning and returns a no-op stop.
//
// transport is the resolved OTLP transport (may be nil); it is required only when
// cfg.Sink == "otlp". Start fails fast with an actionable error when the OTLP
// sink is selected but no endpoint is resolvable.
func Start(
	ctx context.Context,
	cfg *options.LogsConfig,
	transport *options.OTLPConfig,
	deps Deps,
	version, instance string,
	reg prometheus.Registerer,
	selfLogs ...*SelfLogHandler,
) (func(context.Context) error, error) {
	if deps.Logger == nil {
		deps.Logger = slog.Default()
	}
	var selfLog *SelfLogHandler
	for _, candidate := range selfLogs {
		if candidate != nil {
			selfLog = candidate
			break
		}
	}
	sourceLog, pipelineLog := splitDiagnosticLoggers(deps.Logger, selfLog)
	if selfLog != nil {
		if cfg.Sink != "otlp" {
			return nil, fmt.Errorf("self-log shipping requires --logs.sink=otlp; stdout cannot carry OTLP resource attributes")
		}
		deps.Logger = pipelineLog
	}

	// Stamp instance identity onto EVERY logship self-metric (#395). Both registerers
	// are wrapped here, before any source is built: reg carries the pipeline's own
	// metrics, deps.Registerer is what each push receiver registers its shared
	// parse/reject vecs on. main.go currently passes the same registry for both, but
	// wrapping them independently is what makes a future split safe — a receiver
	// registering through an unwrapped deps.Registerer is the exact regression
	// TestStart_WrapsBothRegisterersWithInstanceLabel guards.
	reg = SelfMetricsRegisterer(reg, instance)
	deps.Registerer = SelfMetricsRegisterer(deps.Registerer, instance)

	// Source-owned observations describe the firewall side of the boundary, so
	// they may use the forwarding logger. Delivery-path diagnostics retain
	// pipelineLog above: forwarding a sink failure into the same sink would
	// recurse indefinitely.
	sourceDeps := deps
	sourceDeps.Logger = sourceLog
	sources, err := buildSources(sourceDeps)
	if err != nil {
		return nil, fmt.Errorf("build log sources: %w", err)
	}
	pushSources, err := buildPushSources(deps)
	if err != nil {
		return nil, fmt.Errorf("build push log sources: %w", err)
	}
	if len(sources) == 0 && len(pushSources) == 0 && selfLog == nil {
		deps.Logger.Warn("log shipping enabled but no source is enabled; nothing will be shipped " +
			"(enable a source, e.g. --logs.syslog.enabled)")
		return func(context.Context) error { return nil }, nil
	}

	sink, err := buildSink(cfg, transport, version, instance, deps.Logger)
	if err != nil {
		return nil, err
	}

	pctx, cancel := context.WithCancel(context.Background())
	p := &pipeline{
		sink:                sink,
		cfg:                 cfg,
		log:                 deps.Logger,
		sourceLog:           sourceLog,
		selfLog:             selfLog,
		sources:             sources,
		pushSources:         pushSources,
		stateFile:           cfg.StateFile,
		ctx:                 pctx,
		cancel:              cancel,
		afterSourcesStopped: deps.AfterSourcesStopped,
		limiter:             NewLogLimiter(errorLogInterval, errorLogMaxKeys),
	}
	p.queue = newBoundedQueueBytes(cfg.BufferSize, cfg.BufferMaxBytes, p.noteOverflow)
	sourceLabels := collectSourceNames(sources, pushSources)
	if selfLog != nil {
		sourceLabels.all = append(sourceLabels.all, SelfLogSource)
	}
	p.metrics = newMetrics(reg, queueBounds{
		capacity: cfg.BufferSize,
		maxBytes: cfg.BufferMaxBytes,
		length:   func() float64 { return float64(p.queue.length()) },
		bytes:    func() float64 { return float64(p.queue.queuedBytes()) },
	}, sourceLabels)
	if selfLog != nil {
		selfLog.Bind(func(e Entry) bool {
			e.Record.Attributes = sanitizeAttributes(e.Record.Attributes)
			_, ok := p.enqueue(e)
			return ok
		})
	}

	for _, s := range sources {
		if st, ok := s.(StatefulSource); ok {
			p.stateful = append(p.stateful, st)
		}
	}
	p.loadState()

	p.emitterWG.Add(1)
	go p.runEmitter()

	for _, s := range sources {
		interval := p.effectiveInterval(s)
		p.pollerWG.Add(1)
		go p.runPoller(s, interval)
	}

	// Push sources are registered on pollerWG too, so orderly stop waits for them
	// before closing the shared delivery path. A broken source that ignores
	// cancellation is bounded by the caller's shutdown context instead.
	for _, s := range pushSources {
		p.pollerWG.Add(1)
		go func(s PushSource) {
			defer p.pollerWG.Done()
			p.runPushSource(p.ctx, s)
		}(s)
	}

	if p.stateFile != "" && len(p.stateful) > 0 {
		p.pollerWG.Add(1)
		go p.runStateFlusher()
	}

	names := make([]string, 0, len(sources)+len(pushSources))
	for _, s := range sources {
		names = append(names, s.Name())
	}
	for _, s := range pushSources {
		names = append(names, s.Name())
	}
	deps.Logger.Info("log shipping enabled", "sink", cfg.Sink, "sources", names,
		"poll_interval", cfg.PollInterval.String(), "buffer_size", cfg.BufferSize,
		"buffer_max_bytes", cfg.BufferMaxBytes, "max_record_bytes", cfg.MaxRecordBytes,
		"ship_max_attempts", cfg.ShipMaxAttempts, "ship_max_elapsed", cfg.ShipMaxElapsed.String())

	return p.stop, nil
}

// collectSourceNames derives the label combinations to pre-initialise (#280) from
// the sources actually built. Push sources are excluded from poll: they never call
// Poll, so a poll-error zero for them would be a permanent lie.
func collectSourceNames(sources []Source, pushSources []PushSource) sourceNames {
	names := sourceNames{}
	for _, s := range sources {
		names.all = append(names.all, s.Name())
		names.poll = append(names.poll, s.Name())
		if _, ok := s.(GapReportingSource); ok {
			names.gap = append(names.gap, s.Name())
		}
	}
	for _, s := range pushSources {
		names.all = append(names.all, s.Name())
		if _, ok := s.(GapReportingSource); ok {
			names.gap = append(names.gap, s.Name())
		}
	}
	for _, s := range sources {
		if es, ok := s.(ExtraSourceNames); ok {
			names.all = append(names.all, es.ExtraSourceNames()...)
		}
	}
	for _, s := range pushSources {
		if es, ok := s.(ExtraSourceNames); ok {
			names.all = append(names.all, es.ExtraSourceNames()...)
		}
	}
	return names
}

// noteOverflow is the bounded queue's eviction callback: it counts the evicted
// record on logs_dropped_total{reason="overflow"} and on the console's raw
// throughput total. It is a method rather than an inline closure so both the
// pipeline and the test harness that mirrors Start count overflow identically.
func (p *pipeline) noteOverflow(e Entry) {
	p.metrics.dropped.WithLabelValues(e.Source, dropReasonOverflow).Inc()
	consoleDropped.Add(1)
}

// enqueue is the single ingest gate every record passes through, whether it came
// from a poll Source or a push receiver. It rejects a record whose estimated
// retained size exceeds --logs.max-record-bytes (0 = no cap), counting it under
// reason="record_too_large" instead of queueing it, and returns whether the record
// was accepted.
//
// Rejecting at INGEST rather than at export is the point (#318/#325a): an
// oversized record that reaches the queue both displaces other records under the
// byte budget AND can become a batch the sink permanently refuses, which before
// the attempt cap wedged the single emitter goroutine outright.
// It is also where the exporter-clock receive time is stamped (#394). Being the single
// gate is what makes that stamp trustworthy: it is taken exactly once per record, after
// the size check (so a rejected record carries none and cannot advance receive
// freshness) and before the record can be retried, so it stays byte-identical across
// every export attempt. The caller gets the stamp back so it can advance its own
// pre-hoisted per-source gauge without a second clock read.
func (p *pipeline) enqueue(e Entry) (time.Time, bool) {
	if maxBytes := p.cfg.MaxRecordBytes; maxBytes > 0 && recordBytes(e.Record) > maxBytes {
		p.metrics.dropped.WithLabelValues(e.Source, dropReasonRecordTooLarge).Inc()
		consoleDropped.Add(1)
		if p.limiter.Allow("oversize:" + e.Source) {
			p.log.Warn("log record exceeds the per-record size cap; rejected at ingest",
				"source", e.Source, "bytes", recordBytes(e.Record), "max_record_bytes", maxBytes)
		}
		return time.Time{}, false
	}
	e.Received = time.Now()
	p.queue.push(e)
	return e.Received, true
}

// unixSeconds renders t for a *_timestamp_seconds gauge, keeping sub-second precision.
// Truncating to whole seconds would quantise the received/exported gap — the very thing
// the two gauges exist to measure — to 1s, which is coarser than a healthy pipeline's
// entire queue-to-acknowledgement latency.
func unixSeconds(t time.Time) float64 {
	return float64(t.UnixNano()) / float64(time.Second)
}

// effectiveInterval is max(global poll interval, source floor).
func (p *pipeline) effectiveInterval(s Source) time.Duration {
	interval := p.cfg.PollInterval
	if is, ok := s.(IntervalSource); ok {
		if m := is.MinInterval(); m > interval {
			interval = m
		}
	}
	return interval
}

// runPoller polls a source immediately, then on every tick, until the pipeline
// context is cancelled.
func (p *pipeline) runPoller(s Source, interval time.Duration) {
	defer p.pollerWG.Done()
	name := s.Name()
	t := time.NewTicker(interval)
	defer t.Stop()
	for {
		p.pollOnce(s, name)
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
		}
	}
}

// pollOnce runs one Poll, sanitizes and enqueues the records, and advances the
// source's receive-freshness gauge.
//
// The gauge is set from the last ADMISSION stamp, never from the newest record
// timestamp in the poll (#394). Taking the newest origin timestamp made a poll of old
// or out-of-order data read as a stall, and made the gauge track the data's age rather
// than the source's liveness — which is what an alert on it needs to mean.
func (p *pipeline) pollOnce(s Source, name string) {
	records, err := s.Poll(p.ctx)
	if err != nil {
		p.metrics.pollErrors.WithLabelValues(name).Inc()
		if p.limiter.Allow("poll:" + name) {
			p.sourceLog.Warn("log source poll error", "source", name, "err", err)
		}
		return
	}
	var lastAdmitted time.Time
	for _, r := range records {
		r.Attributes = sanitizeAttributes(r.Attributes)
		recv, ok := p.enqueue(Entry{Source: name, Record: r})
		if !ok {
			continue
		}
		lastAdmitted = recv
	}
	if !lastAdmitted.IsZero() {
		p.metrics.lastReceived.WithLabelValues(name).Set(unixSeconds(lastAdmitted))
	}
}

// shipRetryBase and shipRetryMax bound the exponential backoff between failed
// attempts. Retention is in memory until acknowledgement, elapsed/attempt budget
// exhaustion, or shutdown. Queue overflow, oversized records, terminal rejection,
// budget exhaustion and shutdown abandonment are all counted separately. Process
// restart survival is outside this delivery policy.
const (
	shipRetryBase = 200 * time.Millisecond
	shipRetryMax  = 5 * time.Second
	// shipRateLimitPause is the pacing floor applied after a rate-limit refusal that
	// advertised no Retry-After. It is a real pause rather than the ordinary backoff
	// because a rate limit is a statement about bytes per SECOND: coming straight back
	// with 200ms of backoff is what turns one refusal into ten.
	shipRateLimitPause = 2 * time.Second
	// shipRateLimitMinChunk floors the split. Halving all the way to a single record
	// would trade one rejected request for hundreds of round trips, each paying the
	// fixed per-partition cost, which is a worse failure than the one being fixed. At
	// the measured ~2.7 kB/record on the live box (see the recordBytes block comment in
	// queue.go) 32 records is ~85 kB — small enough that any tenant with a working
	// budget accepts it, large enough that the trip is worth taking.
	shipRateLimitMinChunk = 32
)

// exportByteBound is the byte budget for ONE delivery attempt: the smaller of the
// operator's ingest-rate bound (--logs.max-export-bytes) and the transport backstop
// (maxExportBytes). The two answer different questions and both must hold — see the
// block comment above maxExportBytes. Zero or an out-of-range value falls back to the
// transport backstop, which is the pre-#663 behaviour.
func (p *pipeline) exportByteBound() int {
	n := p.cfg.MaxExportBytes
	if n <= 0 || n > maxExportBytes {
		return maxExportBytes
	}
	return n
}

// runEmitter drains the queue into batches and ships each with bounded in-memory retry.
// It exits when the queue is closed and fully drained.
func (p *pipeline) runEmitter() {
	defer p.emitterWG.Done()
	for {
		batch, ok := p.queue.drainUpTo(p.cfg.BatchMax, p.exportByteBound())
		if !ok {
			return
		}
		p.shipBatch(batch)
	}
}

// shipBatch uses one monotonic elapsed deadline for the entire batch. Every Emit
// and wait shares that deadline; partial success and chunk splitting cannot extend
// it. A positive ShipMaxAttempts adds the existing failed-attempt cap. Acknowledged
// and terminally rejected records leave the pending set immediately, so only the
// retryable remainder and unattempted tail can expire. Retry ambiguity at the wire
// still means at-least-once delivery, not exactly-once.
//
// Shutdown does not abandon the batch on its own. An export in flight when stop
// begins runs on under the stop caller's deadline; a backoff or pace wait ends at
// once and the pending records get their attempt. From then on every chunk gets one
// attempt and any failure, or the stop deadline, abandons the remainder as
// ship_failed. Expiry and attempt-cap exhaustion use ship_failed_permanent. Queue
// overflow continues to be counted separately and may occur before the elapsed limit.
func (p *pipeline) shipBatch(batch []Entry) {
	maxAttempts := p.cfg.ShipMaxAttempts
	maxElapsed := p.cfg.ShipMaxElapsed
	if maxElapsed <= 0 {
		maxElapsed = options.DefaultShipMaxElapsed
	}
	deadline := time.Now().Add(maxElapsed)
	deliveryCtx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	defer p.bindDeliveryToShutdown(cancel)()
	pending := batch
	// chunk starts at the learned cap, or at the whole batch when nothing has been
	// learned — so absent any rate limit the behaviour is exactly the pre-#663 one, one
	// Emit per attempt carrying everything still outstanding.
	chunk := p.shipChunk
	if chunk <= 0 || chunk > len(pending) {
		chunk = len(pending)
	}
	var pace time.Duration
	attempt := 0
	limited := false

	for len(pending) > 0 {
		if time.Until(deadline) <= 0 {
			p.abandonDelivery(pending)
			return
		}
		if pace > 0 && !p.waitOrAbandon(deliveryCtx, pace, pending) {
			return
		}
		n := chunk
		if n < 1 {
			n = 1
		}
		if n > len(pending) {
			n = len(pending)
		}
		head, rest := pending[:n], pending[n:]

		res := p.sink.Emit(deliveryCtx, head)
		p.noteAcked(res.Acked)
		p.noteRejected(res.Rejected)

		if len(res.Retry) == 0 && len(res.Rejected) == 0 {
			// This chunk landed cleanly. Relax the pace geometrically rather than
			// dropping it outright: a tenant that just rate-limited us is still near its
			// budget, and going straight back to full speed is what produces the
			// refuse/retry oscillation instead of a steady drain.
			pace /= 2
			if pace < 100*time.Millisecond {
				pace = 0
			}
			pending = rest
			continue
		}

		// One increment per attempt that did not deliver everything, whatever the
		// reason. It stays an ATTEMPT counter, not a record counter: the records are
		// accounted for by shipped/dropped, and conflating the two would make the
		// three families impossible to reconcile.
		attempt++
		p.metrics.shipErrors.Inc()

		if len(res.Rejected) > 0 && p.limiter.Allow("ship_rejected") {
			p.log.Error("log endpoint terminally rejected records; they are lost and will NOT be retried "+
				"(a permanent protocol response, or an OTLP partial-success rejection)",
				"count", len(res.Rejected), "err", res.Err)
		}

		// Whatever this chunk did not deliver goes back to the FRONT of the outstanding
		// set, ahead of the chunks not yet attempted, so ordering within the batch is
		// preserved across a split.
		pending = joinPending(res.Retry, rest)
		if len(pending) == 0 {
			return
		}

		if p.ctx.Err() != nil || deliveryCtx.Err() != nil || time.Until(deadline) <= 0 {
			p.abandonDelivery(pending)
			return
		}

		if maxAttempts > 0 && attempt >= maxAttempts {
			for _, e := range pending {
				p.metrics.dropped.WithLabelValues(e.Source, dropReasonShipFailedPermanent).Inc()
			}
			consoleDropped.Add(uint64(len(pending)))
			if p.limiter.Allow("ship_permanent") {
				p.log.Error("log sink refused a batch for the maximum number of attempts; dropping it "+
					"so delivery of later batches continues; records lost",
					"attempts", attempt, "count", len(pending), "err", res.Err)
			}
			return
		}

		if delay, isLimited := rateLimitDelay(res.Err); isLimited {
			limited = true
			chunk = splitChunk(n)
			p.shipChunk = chunk
			pace = pacingFor(delay)
			if p.limiter.Allow("ship_rate_limited") {
				p.log.Warn("log destination rate-limited the export; splitting the batch and pacing "+
					"rather than resending it unchanged",
					"attempt", attempt, "refused_records", n, "next_chunk", chunk,
					"pace", pace.String(), "outstanding", len(pending), "err", res.Err)
			}
			// The pace is applied at the top of the loop, so no extra wait here — one
			// delay per attempt, never two stacked.
			continue
		}

		if p.limiter.Allow("ship") {
			p.log.Warn("log sink export failed; retrying", "attempt", attempt, "count", len(pending), "err", res.Err)
		}
		if !p.waitOrAbandon(deliveryCtx, shipBackoff(attempt), pending) {
			return
		}
	}

	// The whole batch was accounted for. If nothing rate-limited us this time, walk the
	// learned cap back UP — geometrically, and only one step per batch, so recovery is
	// slower than the halving that got us here. An adaptive limit that only ever shrinks
	// would leave the pipeline permanently paying per-chunk round-trips for a burst that
	// ended hours ago.
	if !limited {
		p.relaxShipChunk()
	}
}

// relaxShipChunk doubles the learned per-attempt cap, clearing it entirely once it
// reaches the configured batch size — at which point the cap is doing nothing and
// carrying it would just be state to get wrong.
func (p *pipeline) relaxShipChunk() {
	if p.shipChunk <= 0 {
		return
	}
	next := p.shipChunk * 2
	if p.cfg == nil || p.cfg.BatchMax <= 0 || next >= p.cfg.BatchMax {
		p.shipChunk = 0
		return
	}
	p.shipChunk = next
}

// bindDeliveryToShutdown ties one batch's delivery context to shutdown without
// letting producer cancellation cut it off. While the pipeline runs, only the batch's
// own elapsed deadline applies. Once stop cancels p.ctx, cancel fires when the stop
// caller's context ends, so the in-flight export finishes against a healthy endpoint
// and a stuck one is still interrupted at the shutdown deadline. With no stop context
// published (p.ctx cancelled directly), cancel fires at once. The returned function
// releases the registrations and must run when the batch is done.
func (p *pipeline) bindDeliveryToShutdown(cancel context.CancelFunc) func() {
	var mu sync.Mutex
	var released bool
	var unbindStop func() bool
	unbindRun := context.AfterFunc(p.ctx, func() {
		p.stopMu.Lock()
		stopCtx := p.stopCtx
		p.stopMu.Unlock()
		if stopCtx == nil {
			cancel()
			return
		}
		mu.Lock()
		defer mu.Unlock()
		if !released {
			unbindStop = context.AfterFunc(stopCtx, cancel)
		}
	})
	return func() {
		unbindRun()
		mu.Lock()
		defer mu.Unlock()
		released = true
		if unbindStop != nil {
			unbindStop()
		}
	}
}

// waitOrAbandon includes pacing and backoff in the same delivery deadline. When
// shutdown begins during the wait it returns at once so the pending records get
// their final attempt under the stop deadline instead of being abandoned; a wait
// that starts during shutdown is skipped for the same reason.
func (p *pipeline) waitOrAbandon(ctx context.Context, d time.Duration, pending []Entry) bool {
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		p.abandonDelivery(pending)
		return false
	case <-p.ctx.Done():
		if ctx.Err() != nil {
			p.abandonDelivery(pending)
			return false
		}
		return true
	case <-timer.C:
		if ctx.Err() != nil {
			p.abandonDelivery(pending)
			return false
		}
		return true
	}
}

func (p *pipeline) abandonDelivery(pending []Entry) {
	reason := dropReasonShipFailedPermanent
	if p.ctx.Err() != nil {
		reason = dropReasonShipFailed
	}
	for _, e := range pending {
		p.metrics.dropped.WithLabelValues(e.Source, reason).Inc()
	}
	consoleDropped.Add(uint64(len(pending)))
	if p.limiter.Allow(reason) {
		p.log.Error("log delivery budget exhausted or shutdown interrupted delivery; records lost",
			"reason", reason, "count", len(pending))
	}
}

// joinPending concatenates the un-delivered head with the not-yet-attempted tail into a
// fresh slice. It allocates rather than appending in place on purpose: res.Retry is the
// sink's own slice and tail aliases the caller's batch, so appending could write through
// either backing array depending on capacity.
func joinPending(retry, tail []Entry) []Entry {
	if len(retry) == 0 {
		return tail
	}
	if len(tail) == 0 {
		return retry
	}
	out := make([]Entry, 0, len(retry)+len(tail))
	out = append(out, retry...)
	out = append(out, tail...)
	return out
}

// splitChunk halves a refused chunk, floored at shipRateLimitMinChunk — or at 1 when the
// chunk was already at or below that floor, so a rate limit on a single record still
// makes progress rather than looping on an unchanged size.
func splitChunk(n int) int {
	half := n / 2
	if half < shipRateLimitMinChunk {
		if n <= shipRateLimitMinChunk {
			return 1
		}
		return shipRateLimitMinChunk
	}
	return half
}

// pacingFor turns an advertised Retry-After into the delay applied before the next
// chunk. Zero (nothing advertised, the common case) falls back to shipRateLimitPause
// rather than to the ordinary backoff: a bytes/sec budget needs a pause measured in
// seconds, not the 200ms a first-attempt backoff would give.
func pacingFor(advertised time.Duration) time.Duration {
	if advertised < shipRateLimitPause {
		return shipRateLimitPause
	}
	return advertised
}

// noteAcked counts a confirmed delivery and advances export freshness.
//
// One clock read for the whole acknowledgement: every record here was confirmed by the
// same Emit call, so stamping them at different instants would invent a spread that did
// not happen (#394). Export freshness advances HERE and nowhere else — not on enqueue,
// not on a failed attempt, not on a drop, and NOT on a terminal rejection — which is
// what lets a dashboard read a growing received-minus-exported gap as "input is
// arriving but delivery is stuck".
func (p *pipeline) noteAcked(acked []Entry) {
	if len(acked) == 0 {
		return
	}
	ackedAt := unixSeconds(time.Now())
	for _, e := range acked {
		p.metrics.shipped.WithLabelValues(e.Source).Inc()
		p.metrics.lastExported.WithLabelValues(e.Source).Set(ackedAt)
	}
	consoleShipped.Add(uint64(len(acked)))
}

// noteRejected counts records the endpoint terminally refused. They are LOST — counted
// under reason="rejected", never as shipped, and never retried, because a byte-identical
// request would be refused identically. Keeping them distinct from ship_failed_permanent
// matters operationally: that reason means "we gave up after N attempts, the endpoint
// may be sick", while this one means "the endpoint looked at these records and said no",
// which points at the payload or the credentials instead.
func (p *pipeline) noteRejected(rejected []Entry) {
	if len(rejected) == 0 {
		return
	}
	for _, e := range rejected {
		p.metrics.dropped.WithLabelValues(e.Source, dropReasonRejected).Inc()
	}
	consoleDropped.Add(uint64(len(rejected)))
}

// shipBackoff is full-magnitude exponential backoff capped at shipRetryMax, guarded
// against the shift overflowing for a long-running outage.
func shipBackoff(attempt int) time.Duration {
	if attempt < 1 {
		attempt = 1
	}
	if attempt > 8 { // 200ms << 7 already exceeds the 5s cap
		return shipRetryMax
	}
	d := shipRetryBase << (attempt - 1)
	if d > shipRetryMax {
		return shipRetryMax
	}
	return d
}

// runStateFlusher periodically persists cursors while running. The final flush
// runs in stop() after pollers have stopped.
func (p *pipeline) runStateFlusher() {
	defer p.pollerWG.Done()
	t := time.NewTicker(stateFlushInterval)
	defer t.Stop()
	for {
		select {
		case <-p.ctx.Done():
			return
		case <-t.C:
			p.persistState()
		}
	}
}

// stop drains the pipeline in order: stop pollers, flush the queue through the emitter,
// persist final cursors, then flush the sink. Each wait is bounded by ctx: if source
// shutdown does not finish in time, the queue and sink remain open because a live
// producer still owns them; if the emitter drain does not finish in time, stop returns
// an error rather than reporting a clean flush while cursors imply the buffered records
// were delivered (#290). The emitter goroutine still exits shortly after: the batch it
// holds is bound to ctx, so its in-flight export is cancelled at the same deadline.
func (p *pipeline) stop(ctx context.Context) error {
	p.stopMu.Lock()
	p.stopCtx = ctx
	p.stopMu.Unlock()
	if p.selfLog != nil {
		p.selfLog.Unbind()
	}
	p.cancel()
	sourcesStopped := make(chan struct{})
	go func() {
		p.pollerWG.Wait()
		close(sourcesStopped)
	}()
	select {
	case <-sourcesStopped:
	case <-ctx.Done():
		err := fmt.Errorf("log pipeline source shutdown did not finish before shutdown deadline: %w", ctx.Err())
		p.log.Error("log pipeline source shutdown did not finish before shutdown deadline; leaving delivery resources open while a producer remains live", "err", err)
		return err
	}
	if p.afterSourcesStopped != nil {
		p.afterSourcesStopped()
	}
	p.queue.close()

	drained := make(chan struct{})
	go func() {
		p.emitterWG.Wait()
		close(drained)
	}()
	var drainErr error
	select {
	case <-drained:
	case <-ctx.Done():
		drainErr = fmt.Errorf("log pipeline drain did not finish before shutdown deadline: %w", ctx.Err())
		p.log.Error("log pipeline shutdown timed out before the queue fully drained; buffered records may be lost")
	}

	p.persistState()
	return errors.Join(drainErr, p.sink.Shutdown(ctx))
}

// loadState reads the state file (if any) and restores each stateful source's
// cursor. A missing or unreadable file is not fatal: sources resume from now.
func (p *pipeline) loadState() {
	if p.stateFile == "" || len(p.stateful) == 0 {
		return
	}
	data, err := os.ReadFile(p.stateFile)
	if err != nil {
		if !os.IsNotExist(err) {
			p.log.Warn("could not read log state file; resuming from now", "path", p.stateFile, "err", err)
		}
		return
	}
	var blobs map[string]string
	if err := json.Unmarshal(data, &blobs); err != nil {
		p.log.Warn("log state file is corrupt; resuming from now", "path", p.stateFile, "err", err)
		return
	}
	for _, st := range p.stateful {
		enc, ok := blobs[st.Name()]
		if !ok {
			continue
		}
		raw, derr := base64.StdEncoding.DecodeString(enc)
		if derr != nil {
			p.log.Warn("log state entry is corrupt; resuming from now", "source", st.Name(), "err", derr)
			continue
		}
		st.LoadState(raw)
	}
	p.lastPersisted = data
}

// persistState snapshots every stateful source's cursor and atomically rewrites
// the state file, but only when the snapshot differs from the last write (so
// idle sources cause no I/O).
func (p *pipeline) persistState() {
	if p.stateFile == "" || len(p.stateful) == 0 {
		return
	}
	p.stateMu.Lock()
	defer p.stateMu.Unlock()

	blobs := make(map[string]string, len(p.stateful))
	for _, st := range p.stateful {
		data, ok := st.SaveState()
		if !ok {
			continue
		}
		blobs[st.Name()] = base64.StdEncoding.EncodeToString(data)
	}
	if len(blobs) == 0 {
		return
	}
	encoded, err := json.Marshal(blobs)
	if err != nil {
		p.log.Warn("could not encode log state", "err", err)
		return
	}
	if p.lastPersisted != nil && string(encoded) == string(p.lastPersisted) {
		return
	}
	if err := atomicWrite(p.stateFile, encoded); err != nil {
		p.log.Warn("could not write log state file", "path", p.stateFile, "err", err)
		return
	}
	p.lastPersisted = encoded
}

// atomicWrite writes data to a sibling temp file and renames it over path, so a
// reader never sees a partial state file.
func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
