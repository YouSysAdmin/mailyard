// Mailyard, Copyright (c) 2021-2026 YouSysAdmin

// Package metrics exposes Prometheus instrumentation. Counters are
// package-level so instrumented code paths do not need wiring - the
// /metrics route is registered only when metrics.enabled is true, and
// an unexported registry keeps the default Go runtime collectors.
package metrics

import (
	"context"
	"time"

	"net/http"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var (
	// EmailsAccepted counts sends accepted into the queue (any
	// surface: console, machine API, relay, campaigns).
	EmailsAccepted = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailyard_emails_accepted_total",
		Help: "Emails accepted into the delivery queue.",
	})

	// EmailsFinalized counts terminal delivery outcomes by status
	// (sent, failed, suppressed).
	EmailsFinalized = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailyard_emails_finalized_total",
		Help: "Emails reaching a terminal status.",
	}, []string{"status"})

	// InboundReceived counts stored inbound messages.
	InboundReceived = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailyard_inbound_received_total",
		Help: "Inbound messages stored for a verified domain.",
	})

	// SandboxCaptures counts messages captured into a project sandbox
	// rather than delivered. Its own counter, not a label on
	// EmailsAccepted, because these never enter the queue and folding
	// them in would make a CI run look like sending volume.
	SandboxCaptures = promauto.NewCounter(prometheus.CounterOpts{
		Name: "mailyard_sandbox_captures_total",
		Help: "Messages captured into a project sandbox instead of being delivered.",
	})

	// WebhookDeliveries counts webhook delivery attempts by outcome.
	WebhookDeliveries = promauto.NewCounterVec(prometheus.CounterOpts{
		Name: "mailyard_webhook_deliveries_total",
		Help: "Outgoing webhook deliveries by outcome.",
	}, []string{"status"})
)

// QueueCounter is the scrape-time source for queue depth: a function
// returning email counts by status across all projects.
type QueueCounter func(ctx context.Context) (map[string]int, error)

type queueCollector struct {
	count QueueCounter
	desc  *prometheus.Desc
}

// Describe implements prometheus.Collector.
func (c *queueCollector) Describe(ch chan<- *prometheus.Desc) { ch <- c.desc }

// Collect implements prometheus.Collector, sampling the current
// values.
func (c *queueCollector) Collect(ch chan<- prometheus.Metric) {
	counts, err := c.count(context.Background())
	if err != nil {
		return
	}

	for status, n := range counts {
		ch <- prometheus.MustNewConstMetric(c.desc, prometheus.GaugeValue, float64(n), status)
	}
}

// RegisterQueueCollector adds the scrape-time email status gauge.
func RegisterQueueCollector(count QueueCounter) {
	prometheus.MustRegister(&queueCollector{
		count: count,
		desc: prometheus.NewDesc("mailyard_emails_by_status",
			"Current email rows by status across all projects.", []string{"status"}, nil),
	})
}

// PartitionCounter is the scrape-time source for how many daily
// partitions the emails table has and how many this installation
// tolerates.
type PartitionCounter func(ctx context.Context) (partitions, ceiling int, err error)

type partitionCollector struct {
	count   PartitionCounter
	current *prometheus.Desc
	ceiling *prometheus.Desc
}

// Describe implements prometheus.Collector.
func (c *partitionCollector) Describe(ch chan<- *prometheus.Desc) {
	ch <- c.current
	ch <- c.ceiling
}

// Collect implements prometheus.Collector, sampling the current
// values.
//
// Bounded, because Collect runs inside the scrape: an unbounded
// catalog query against a wedged database would hang the whole
// /metrics response, which is the endpoint an operator is reading
// precisely when the database is wedged.
func (c *partitionCollector) Collect(ch chan<- prometheus.Metric) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	n, ceiling, err := c.count(ctx)
	if err != nil {
		return
	}

	ch <- prometheus.MustNewConstMetric(c.current, prometheus.GaugeValue, float64(n))
	ch <- prometheus.MustNewConstMetric(c.ceiling, prometheus.GaugeValue, float64(ceiling))
}

// RegisterPartitionCollector adds the partition count gauges.
//
// BOTH numbers, because the count alone cannot be alerted on: the
// ceiling is overridable per installation, so a rule written against a
// literal 400 is wrong on the installation that raised it. Exported as
// a pair, an operator writes
// mailyard_email_partitions / mailyard_email_partitions_ceiling > 0.8
// and it stays true wherever it is deployed.
func RegisterPartitionCollector(count PartitionCounter) {
	prometheus.MustRegister(&partitionCollector{
		count: count,
		current: prometheus.NewDesc("mailyard_email_partitions",
			"Daily partitions on the emails table.", nil, nil),
		ceiling: prometheus.NewDesc("mailyard_email_partitions_ceiling",
			"Partition count past which concurrent queue claims start failing.", nil, nil),
	})
}

// HTTPHandler returns the standard promhttp scrape handler.
func HTTPHandler() http.Handler {
	return promhttp.Handler()
}
