package main

import (
	"context"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.21.0"
)

func initTracer(ctx context.Context) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		endpoint = "simplest-collector:4318"
	}

	exporter, err := otlptracehttp.New(ctx,
		otlptracehttp.WithEndpoint(endpoint),
		otlptracehttp.WithInsecure(),
	)
	if err != nil {
		return nil, fmt.Errorf("create otlp exporter: %w", err)
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("order-service"),
			semconv.ServiceVersion("1.0.0"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("create resource: %w", err)
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	return tp.Shutdown, nil
}

func main() {
	ctx := context.Background()

	shutdown, err := initTracer(ctx)
	if err != nil {
		log.Fatalf("init tracer: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			log.Printf("shutdown tracer: %v", err)
		}
	}()

	calcURL := os.Getenv("CALCULATION_SERVICE_URL")
	if calcURL == "" {
		calcURL = "http://service-b:8080/calculate"
	}

	httpClient := &http.Client{
		Transport: otelhttp.NewTransport(http.DefaultTransport),
	}
	tracer := otel.Tracer("order-service")

	handler := func(w http.ResponseWriter, r *http.Request) {
		ctx := r.Context()

		ctx, span := tracer.Start(ctx, "create-order")
		defer span.End()

		span.SetAttributes(
			attribute.String("order.id", "ORD-2024-001"),
			attribute.String("order.source", "b2c"),
			attribute.String("order.status", "SUBMITTED"),
		)

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, calcURL, nil)
		if err != nil {
			span.RecordError(err)
			http.Error(w, "build request: "+err.Error(), http.StatusInternalServerError)
			return
		}

		resp, err := httpClient.Do(req)
		if err != nil {
			span.RecordError(err)
			http.Error(w, "call calculation-service: "+err.Error(), http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()

		body, _ := io.ReadAll(resp.Body)

		span.SetAttributes(attribute.String("order.status", "PRICE_CALCULATED"))

		fmt.Fprintf(w, "[order-service] order ORD-2024-001 created\n[calculation-service] %s", string(body))
	}

	http.Handle("/", otelhttp.NewHandler(http.HandlerFunc(handler), "order.create"))

	log.Println("order-service listening on :8080")
	log.Fatal(http.ListenAndServe(":8080", nil))
}
