package main

import (
	"encoding/json"
	`flag`
	`github.com/erwinvaneyk/simfaas`
	`github.com/prometheus/client_golang/prometheus`
	`github.com/prometheus/client_golang/prometheus/promhttp`
	"log"
	"net/http"
	`regexp`
	"time"
)

var (
	// buildTime is a UNIX datetime that should be injected at build time.
	buildTime string // UNIX
	
	//
	// HTTP server metrics
	//
	
	requestCount = prometheus.NewCounterVec(prometheus.CounterOpts{
		Namespace: "simfaas",
		Name:      "api_request_count",
		Help:      "Number of requests to an endpoint.",
	}, []string{"path", "code", "method"})
	
	requestDuration = prometheus.NewSummaryVec(prometheus.SummaryOpts{
		Namespace: "simfaas",
		Name:      "api_request_duration",
		Help:      "Duration of the request processing in seconds.",
		Objectives: map[float64]float64{
			0:    0.001,
			0.01: 0.001,
			0.02: 0.001,
			0.1:  0.01,
			0.25: 0.01,
			0.5:  0.01,
			0.75: 0.01,
			0.9:  0.01,
			0.98: 0.001,
			0.99: 0.001,
			1:    0.001,
		},
	}, []string{"path", "code", "method"})
	
	//
	// Execution metrics
	//
	
	// TODO also include function name
	executionStatus = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "simfaas",
		Name:      "executions",
		Help:      "Number of function executions inflight.",
	}, []string{"status"})
	
	//
	// (Simulated) Resource Metrics
	//
	
	fnResources = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "simfaas",
		Name:      "fn_resource_usage",
		Help:      "Current simulated resource usage of functions.",
	})
	
	fnResourceUsage = prometheus.NewCounter(prometheus.CounterOpts{
		Namespace: "simfaas",
		Name:      "fn_resource_usage_total",
		Help:      "Total simulated resource usage of functions.",
	})
)

func init() {
	prometheus.MustRegister(requestCount, requestDuration, executionStatus, fnResources, fnResourceUsage)
}

func main() {
	// Parse arguments
	coldStart := flag.Duration("cold-start", 0, "The default cold start duration")
	keepWarm := flag.Duration("keep-warm", 0, "How long the function should be kept warm after an execution.")
	addr := flag.String("addr", ":8888", "Address serve the API on.")
	flag.Parse()
	log.Printf("simfission %s", buildTime)
	
	useColdStarts := *coldStart > 0 || *keepWarm > 0
	if !useColdStarts {
		log.Println("cold starts disabled. Cold start duration or keep warm duration should be larger than zero.")
	} else {
		log.Printf("cold starts enabled (cold start: %s, keep-warm: %s)", (*coldStart).String(),
			(*keepWarm).String())
	}
	
	// Setup simulator
	fission := simfaas.Fission{
		Platform:                 simfaas.New(),
		CreateUndefinedFunctions: true,
		FnFactory: func(fnName string) *simfaas.FunctionConfig {
			return &simfaas.FunctionConfig{
				ColdStart: *coldStart,
				KeepWarm:  *keepWarm,
			}
		},
	}
	if err := fission.Start(); err != nil {
		log.Fatalf("Failed to start FaaS simulator: %v", err)
	}
	defer fission.Close()
	
	// Publish FaaS simulator resource usage to Prometheus
	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			<-ticker.C
			activeFns := fission.Platform.ActiveFunctionInstances()
			fnResourceUsage.Add(float64(activeFns))
			fnResources.Set(float64(activeFns))
			executionStatus.WithLabelValues("queued").Set(float64(fission.Platform.QueuedExecutions()))
			executionStatus.WithLabelValues("active").Set(float64(fission.Platform.ActiveExecutions()))
		}
	}()
	
	// Application info
	versionHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bs, err := json.Marshal(struct {
			Name      string
			BuildTime string
		}{
			Name:      "simfission",
			BuildTime: buildTime,
		})
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			panic(err)
		}
		if _, err := w.Write(bs); err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			panic(err)
		}
	})
	
	// Collect and expose metrics
	mux := &simfaas.RegexpHandler{}
	instrumentEndpoint(mux, regexp.MustCompile("/"), versionHandler)
	mux.Handler(regexp.MustCompile("/metrics"), promhttp.Handler())
	instrumentEndpoint(mux, regexp.MustCompile("/v2/functions/.*"), http.HandlerFunc(fission.HandleFunctionsGet))
	instrumentEndpoint(mux, regexp.MustCompile("/v2/tapService"), http.HandlerFunc(fission.HandleTapService))
	instrumentEndpoint(mux, regexp.MustCompile("/v2/getServiceForFunction"), http.HandlerFunc(fission.HandleGetServiceForFunction))
	instrumentEndpoint(mux, regexp.MustCompile("/fission-function/.*"), http.HandlerFunc(fission.HandleFunctionRun))
	
	// Start serving
	log.Printf("Serving at %s", *addr)
	log.Fatal((&http.Server{
		Addr:        *addr,
		ReadTimeout: 5 * time.Second,
		Handler:     mux,
	}).ListenAndServe())
}

func instrumentEndpoint(mux *simfaas.RegexpHandler, regexPath *regexp.Regexp, handler http.Handler) {
	path := regexPath.String()
	mux.Handler(regexPath, promhttp.InstrumentHandlerCounter(requestCount.MustCurryWith(prometheus.Labels{"path": path}),
		promhttp.InstrumentHandlerDuration(requestDuration.MustCurryWith(prometheus.Labels{"path": path}), handler)))
}
