module soll/devnull

go 1.16

require (
	go.opencensus.io v0.23.0
	go.opentelemetry.io/otel v1.7.0
	go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc v1.7.0
	go.opentelemetry.io/otel/exporters/zipkin v1.7.0
	go.opentelemetry.io/otel/sdk v1.7.0
	go.opentelemetry.io/otel/trace v1.7.0 // indirect
	google.golang.org/grpc v1.46.0
	//gopkg.in/Graylog2/go-gelf.v1 v1.0.0-20170811154226-7ebf4f536d8f // indirect
	gopkg.in/Graylog2/go-gelf.v2 v2.0.0-20191017102106-1550ee647df0
	gopkg.in/yaml.v2 v2.4.0
)
