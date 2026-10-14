package main

// Managed dependencies (a Kafka like MSK or Confluent Cloud, a MongoDB like
// Atlas) live outside the platform: teia only sees them through the client
// spans of the services that use them. The lab simulates exactly that, so it
// needs no broker or database containers for them.

import (
	"context"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// peerSystem parses "peer=system", e.g. "order-events=kafka".
func peerSystem(v string) (peer, system string, ok bool) {
	peer, system, ok = strings.Cut(strings.TrimSpace(v), "=")
	return peer, system, ok && peer != "" && system != ""
}

// publish records a producer span to a broker, as messaging instrumentation does.
func publish(ctx context.Context, spec string) {
	peer, system, ok := peerSystem(spec)
	if !ok {
		return
	}
	_, span := tracer.Start(ctx, "publish "+peer, trace.WithSpanKind(trace.SpanKindProducer), trace.WithAttributes(
		attribute.String("messaging.system", system),
		attribute.String("messaging.destination.name", peer),
		attribute.String("peer.service", peer),
	))
	time.Sleep(2 * time.Millisecond)
	span.End()
}

// docQuery records a client span to a document database.
func docQuery(ctx context.Context, spec string) {
	peer, system, ok := peerSystem(spec)
	if !ok {
		return
	}
	_, span := tracer.Start(ctx, "find products", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
		attribute.String("db.system", system),
		attribute.String("db.system.name", system),
		attribute.String("peer.service", peer),
	))
	time.Sleep(3 * time.Millisecond)
	span.End()
}

// consume is a worker polling a broker and calling an external API per
// message: ROLE=consumer, CONSUME=order-events=kafka, NOTIFY=email-provider.
func consume(ctx context.Context) error {
	interval, _ := time.ParseDuration(env("INTERVAL", "2s"))
	ticker := time.NewTicker(interval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
		peer, system, ok := peerSystem(os.Getenv("CONSUME"))
		if !ok {
			continue
		}
		pctx, poll := tracer.Start(ctx, "poll "+peer, trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
			attribute.String("messaging.system", system),
			attribute.String("messaging.destination.name", peer),
			attribute.String("peer.service", peer),
		))
		time.Sleep(2 * time.Millisecond)
		poll.End()
		if notify := os.Getenv("NOTIFY"); notify != "" {
			_, send := tracer.Start(pctx, "POST /v3/mail/send", trace.WithSpanKind(trace.SpanKindClient), trace.WithAttributes(
				attribute.String("http.request.method", "POST"),
				attribute.String("peer.service", notify),
			))
			time.Sleep(15 * time.Millisecond)
			send.End()
		}
	}
}
