package main

import (
	"bytes"
	"context"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/signal"
	"sort"
	"syscall"
	"time"

	//"gopkg.in/Graylog2/go-gelf.v1/gelf"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/zipkin"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.10.0"
	"go.opentelemetry.io/otel/trace"
	"gopkg.in/Graylog2/go-gelf.v2/gelf"
	"gopkg.in/yaml.v2"
)

// Config struct for webapp config
type Config struct {
	Server struct {
		// Host is the local machine IP Address to bind the HTTP Server to
		Host string `yaml:"host"`

		// Port is the local machine TCP Port to bind the HTTP Server to
		Port    string `yaml:"port"`
		Timeout struct {
			// Server is the general server timeout to use
			// for graceful shutdowns
			Server time.Duration `yaml:"server"`

			// Write is the amount of time to wait until an HTTP server
			// write opperation is cancelled
			Write time.Duration `yaml:"write"`

			// Read is the amount of time to wait until an HTTP server
			// read operation is cancelled
			Read time.Duration `yaml:"read"`

			// Read is the amount of time to wait
			// until an IDLE HTTP session is closed
			Idle time.Duration `yaml:"idle"`
		} `yaml:"timeout"`
	} `yaml:"server"`
	Graylog struct {
		Host string `yaml:"host"`
		Port string `yaml:"port"`
	} `yaml:"graylog"`
	Zipkin struct {
		Url string `yaml:"url"`
	} `yaml:"zipkin"`
}

// NewConfig returns a new decoded Config struct
func NewConfig(configPath string) (*Config, error) {
	// Create config structure
	config := &Config{}

	// Open config file
	file, err := os.Open(configPath)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	// Init new YAML decode
	d := yaml.NewDecoder(file)

	// Start YAML decoding from file
	if err := d.Decode(&config); err != nil {
		return nil, err
	}

	return config, nil
}

// ValidateConfigPath just makes sure, that the path provided is a file,
// that can be read
func ValidateConfigPath(path string) error {
	s, err := os.Stat(path)
	if err != nil {
		return err
	}
	if s.IsDir() {
		return fmt.Errorf("'%s' is a directory, not a normal file", path)
	}
	return nil
}

// ParseFlags will create and parse the CLI flags
// and return the path to be used elsewhere
func ParseFlags() (string, error) {
	// String that contains the configured configuration path
	var configPath string

	// Set up a CLI flag called "-config" to allow users
	// to supply the configuration file
	flag.StringVar(&configPath, "config", "./config.yaml", "path to config file")

	// Actually parse the flags
	flag.Parse()

	// Validate the path first
	if err := ValidateConfigPath(configPath); err != nil {
		return "", err
	}

	// Return the configuration path
	return configPath, nil
}
func Tracing(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header["X-B3-Traceid"] != nil && r.Header["X-B3-Spanid"] != nil && r.Header["X-B3-Sampled"] != nil {

			traceID, _ := trace.TraceIDFromHex(r.Header["X-B3-Traceid"][0])
			spanID, _ := trace.SpanIDFromHex(r.Header["X-B3-Spanid"][0])
			var traceFlags trace.TraceFlags
			if r.Header["X-B3-Sampled"][0] == "1" {
				traceFlags = trace.FlagsSampled
			}

			spanContext := trace.NewSpanContext(trace.SpanContextConfig{
				TraceID:    traceID,
				SpanID:     spanID,
				TraceFlags: traceFlags,
			})

			ctx := trace.ContextWithSpanContext(r.Context(), spanContext)

			r = r.WithContext(ctx)
		}
		next.ServeHTTP(w, r)
	})
}

// NewRouter generates the router used in the HTTP Server
func NewRouter(hostname string) *http.ServeMux {
	// Create router and define routes and return that router
	router := http.NewServeMux()
	tr := otel.GetTracerProvider().Tracer("devnull")

	devnuller := func(w http.ResponseWriter, r *http.Request) {
		ctx2, span := tr.Start(r.Context(), "foo", trace.WithSpanKind(trace.SpanKindServer))
		defer span.End()

		var resp bytes.Buffer
		resp.WriteString(fmt.Sprintf("Timestamp: %s\n", time.Now()))
		resp.WriteString("---------\n")
		resp.WriteString(fmt.Sprintf("Hello, you've requested: %s on Pod: %s \n", r.URL.Path, hostname))
		resp.WriteString("---------\n")
		resp.WriteString(fmt.Sprintf("The Methode %q\n", r.Method))

		resp.WriteString("---------\n")

		keys := make([]string, 0, len(r.URL.Query()))

		_, span2 := tr.Start(ctx2, "foo2", trace.WithSpanKind(trace.SpanKindServer))
		defer span2.End()
		for k := range r.URL.Query() {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, v := range keys {
			resp.WriteString(fmt.Sprintf("Query field %q, Value %q\n", v, r.URL.Query()[v]))
		}

		resp.WriteString("---------\n")

		headers := make([]string, 0, len(r.Header))
		for h := range r.Header {
			headers = append(headers, h)
		}
		sort.Strings(headers)
		for _, v := range headers {
			resp.WriteString(fmt.Sprintf("Header field %q, Value %q\n", v, r.Header[v]))
		}

		resp.WriteString("---------\n")
		if r.Body == nil {
			resp.WriteString(fmt.Sprintf("There's no body"))
		} else {
			resp.WriteString(fmt.Sprintf("The Body is %q\n", r.Body))
		}
		fmt.Fprintf(w, resp.String())
		log.Printf(resp.String())
	}

	finalHandler := http.HandlerFunc(devnuller)
	router.Handle("/", Tracing(finalHandler))

	return router
}

// Run will run the HTTP Server
func (config Config) Run() {

	hostname, err := os.Hostname()
	if err != nil {
		log.Fatal(err)
	}

	// Set up a channel to listen to for interrupt signals
	var runChan = make(chan os.Signal, 1)

	// Set up a context to allow for graceful server shutdowns in the event
	// of an OS interrupt (defers the cancel just in case)
	ctx, cancel := context.WithTimeout(
		context.Background(),
		config.Server.Timeout.Server,
	)
	defer cancel()

	// Define server options
	server := &http.Server{
		Addr:         config.Server.Host + ":" + config.Server.Port,
		Handler:      NewRouter(hostname),
		ReadTimeout:  config.Server.Timeout.Read * time.Second,
		WriteTimeout: config.Server.Timeout.Write * time.Second,
		IdleTimeout:  config.Server.Timeout.Idle * time.Second,
	}

	// Handle ctrl+c/ctrl+x interrupt
	signal.Notify(runChan, os.Interrupt, syscall.SIGTSTP)

	// Alert the user that the server is starting
	log.Printf("Server is starting on %s\n", server.Addr)

	// Run the server on a new goroutine
	go func() {
		if err := server.ListenAndServe(); err != nil {
			if err == http.ErrServerClosed {
				// Normal interrupt operation, ignore
			} else {
				log.Fatalf("Server failed to start due to err: %v", err)
			}
		}
	}()

	// Block on this channel listeninf for those previously defined syscalls assign
	// to variable so we can let the user know why the server is shutting down
	interrupt := <-runChan

	// If we get one of the pre-prescribed syscalls, gracefully terminate the server
	// while alerting the user
	log.Printf("Server is shutting down due to %+v\n", interrupt)
	if err := server.Shutdown(ctx); err != nil {
		log.Fatalf("Server was unable to gracefully shutdown due to err: %+v", err)
	}
}

// Func main should be as small as possible and do as little as possible by convention
func main() {
	// Generate our config based on the config supplied
	// by the user in the flags
	cfgPath, err := ParseFlags()
	if err != nil {
		log.Fatal(err)
	}

	cfg, err := NewConfig(cfgPath)
	if err != nil {
		log.Fatal(err)
	}
	if cfg.Graylog.Host != "" {

		gelfWriter, err := gelf.NewUDPWriter(cfg.Graylog.Host + ":" + cfg.Graylog.Port)
		if err != nil {
			log.Fatalf("gelf.NewWriter: %s", err)
		}
		gelfWriter.Facility = "devnull-Service"

		// log to both stderr and graylog2
		//	log.SetOutput(io.MultiWriter(os.Stderr, gelfWriter))
		log.SetOutput(io.MultiWriter(gelfWriter))
		log.Printf("logging to stderr & graylog@'%s'", cfg.Graylog.Host)
	}

	// Tracing
	initTracer(cfg.Zipkin.Url)

	// Run the server
	cfg.Run()
}

func initTracer(url string) (func(context.Context) error, error) {
	// Create Zipkin Exporter and install it as a global tracer.
	//
	// For demoing purposes, always sample. In a production application, you should
	// configure the sampler to a trace.ParentBased(trace.TraceIDRatioBased) set at the desired
	// ratio.
	exporter, err := zipkin.New(url)
	if err != nil {
		return nil, err
	}

	batcher := sdktrace.NewBatchSpanProcessor(exporter)

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithSpanProcessor(batcher),
		sdktrace.WithResource(resource.NewWithAttributes(
			semconv.SchemaURL,
			semconv.ServiceNameKey.String("devnull"),
		)),
	)
	otel.SetTracerProvider(tp)

	return tp.Shutdown, nil
}
