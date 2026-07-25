package observability

import (
	"context"
	"strings"
	"unicode/utf8"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/sdk/trace"
)

// invalidUTF8Replacement replaces runs of invalid UTF-8 bytes. U+FFFD is the
// Unicode replacement character, so a poisoned value stays greppable in Tempo
// instead of vanishing.
const invalidUTF8Replacement = "�"

// sanitizingExporter wraps a SpanExporter and strips invalid UTF-8 from span
// names, attributes, events and links before the batch is marshalled.
//
// OTLP is protobuf, whose string fields must be valid UTF-8. A single bad byte
// fails the entire batch with "string field contains invalid UTF-8" and every
// span in it is lost. Spans carry request-derived, attacker-controlled values
// (url.path and user_agent.original from otelhttp, MCP tool names), so this is
// the trust boundary between untrusted input and the export pipeline. It is the
// trace-side counterpart of the utf8.ValidString guard in MetricsMiddleware.
type sanitizingExporter struct {
	trace.SpanExporter
}

func (e sanitizingExporter) ExportSpans(ctx context.Context, spans []trace.ReadOnlySpan) error {
	var out []trace.ReadOnlySpan
	for i, s := range spans {
		if spanIsValidUTF8(s) {
			continue
		}
		// First offender: copy the clean prefix, then sanitize from here on.
		// Valid batches (the overwhelming majority) allocate nothing.
		if out == nil {
			out = make([]trace.ReadOnlySpan, len(spans))
			copy(out, spans[:i])
		}
		out[i] = sanitizedSpan{ReadOnlySpan: s}
	}
	if out == nil {
		return e.SpanExporter.ExportSpans(ctx, spans)
	}
	// Fill the untouched tail left by the loop above.
	for i, s := range spans {
		if out[i] == nil {
			out[i] = s
		}
	}
	return e.SpanExporter.ExportSpans(ctx, out)
}

// sanitizedSpan overrides only the string-bearing accessors. Embedding the
// interface promotes ReadOnlySpan's unexported private() method, which is what
// lets a type outside the SDK satisfy it.
type sanitizedSpan struct {
	trace.ReadOnlySpan
}

func (s sanitizedSpan) Name() string {
	return toValidUTF8(s.ReadOnlySpan.Name())
}

func (s sanitizedSpan) Attributes() []attribute.KeyValue {
	return sanitizeAttrs(s.ReadOnlySpan.Attributes())
}

func (s sanitizedSpan) Events() []trace.Event {
	events := s.ReadOnlySpan.Events()
	for i := range events {
		events[i].Name = toValidUTF8(events[i].Name)
		events[i].Attributes = sanitizeAttrs(events[i].Attributes)
	}
	return events
}

func (s sanitizedSpan) Links() []trace.Link {
	links := s.ReadOnlySpan.Links()
	for i := range links {
		links[i].Attributes = sanitizeAttrs(links[i].Attributes)
	}
	return links
}

// spanIsValidUTF8 reports whether a span can be marshalled as-is.
func spanIsValidUTF8(s trace.ReadOnlySpan) bool {
	if !utf8.ValidString(s.Name()) || !attrsAreValidUTF8(s.Attributes()) {
		return false
	}
	for _, e := range s.Events() {
		if !utf8.ValidString(e.Name) || !attrsAreValidUTF8(e.Attributes) {
			return false
		}
	}
	for _, l := range s.Links() {
		if !attrsAreValidUTF8(l.Attributes) {
			return false
		}
	}
	return true
}

func attrsAreValidUTF8(attrs []attribute.KeyValue) bool {
	for _, a := range attrs {
		if !utf8.ValidString(string(a.Key)) {
			return false
		}
		switch a.Value.Type() {
		case attribute.STRING:
			if !utf8.ValidString(a.Value.AsString()) {
				return false
			}
		case attribute.STRINGSLICE:
			for _, v := range a.Value.AsStringSlice() {
				if !utf8.ValidString(v) {
					return false
				}
			}
		}
	}
	return true
}

func sanitizeAttrs(attrs []attribute.KeyValue) []attribute.KeyValue {
	for i, a := range attrs {
		attrs[i].Key = attribute.Key(toValidUTF8(string(a.Key)))
		switch a.Value.Type() {
		case attribute.STRING:
			attrs[i].Value = attribute.StringValue(toValidUTF8(a.Value.AsString()))
		case attribute.STRINGSLICE:
			vs := a.Value.AsStringSlice()
			for j, v := range vs {
				vs[j] = toValidUTF8(v)
			}
			attrs[i].Value = attribute.StringSliceValue(vs)
		}
	}
	return attrs
}

func toValidUTF8(s string) string {
	if utf8.ValidString(s) {
		return s
	}
	return strings.ToValidUTF8(s, invalidUTF8Replacement)
}
