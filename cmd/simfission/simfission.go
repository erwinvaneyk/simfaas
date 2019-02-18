package main

import (
	"encoding/json"
	`flag`
	`github.com/prometheus/client_golang/prometheus`
	`github.com/prometheus/client_golang/prometheus/promhttp`
	`go.uber.org/atomic`
	"io/ioutil"
	"log"
	"net/http"
	`strconv`
	`sync`
	"time"
)

// simfaas - is a very simple mock of a FaaS platform to implement the sleep function with minimal interference

var (
	buildTime string // UNIX
	
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
			0.1:  0.01,
			0.25: 0.01,
			0.5:  0.05,
			0.75: 0.01,
			0.9:  0.01,
			0.99: 0.001,
			1:    0.001,
		},
	}, []string{"path", "code", "method"})
	
	requestInFlight = prometheus.NewGaugeVec(prometheus.GaugeOpts{
		Namespace: "simfaas",
		Name:      "api_request_inflight",
		Help:      "Number of requests in-flight.",
	}, []string{"path"})
	
	//
	// (Simulated) Resource Metrics
	//
	
	fnResources = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "simfaas",
		Name:      "fn_resource_usage",
		Help:      "Current simulated resource usage of functions.",
	})
	
	fnResourceUsage = prometheus.NewGauge(prometheus.GaugeOpts{
		Namespace: "simfaas",
		Name:      "fn_resource_usage_total",
		Help:      "Total simulated resource usage of functions.",
	})
)

type Function struct {
	name       string
	lastExec   time.Time
	deployedAt time.Time
}

func init() {
	prometheus.MustRegister(requestCount, requestDuration, requestInFlight, fnResources, fnResourceUsage)
}

// TODO how to differentiate functions for prewarming? Probably include fn in query of workflow run.
func main() {
	// Parse arguments
	coldStartDuration := flag.Duration("cold-start-duration", 0, "The default cold start duration")
	keepWarmDuration := flag.Duration("keep-warm-duration", 0,
		"How long the function should be kept warm after an execution.")
	addr := flag.String("addr", ":8888", "Address to bind the server to.")
	flag.Parse()
	useColdStarts := *coldStartDuration > 0 || *keepWarmDuration > 0
	if !useColdStarts {
		log.Println("cold starts disabled. Cold start duration or keep warm duration should be larger than zero.")
	} else {
		log.Printf("cold starts enabled (cold start: %s, keep-warm: %s)", (*coldStartDuration).String(),
			(*keepWarmDuration).String())
	}
	
	// Setup server
	mux := http.NewServeMux()
	fnCache := sync.Map{} // map[string]Function
	fnActive := atomic.Int32{}
	
	// Function GC
	if useColdStarts {
		tick := time.NewTicker(10 * time.Second)
		go func() {
			<-tick.C
			n := time.Now()
			
			fnCache.Range(func(k, v interface{}) bool {
				entry := v.(Function)
				if entry.lastExec.Add(*keepWarmDuration).Before(n) {
					log.Printf("Cleaned-up fn %s", k)
					fnCache.Delete(k)
					fnActive.Dec()
				}
				return true
			})
		}()
	}
	
	// Resource simulator
	go func() {
		ticker := time.NewTicker(time.Second)
		for {
			<-ticker.C
			activeFns := fnActive.Load()
			fnResourceUsage.Add(float64(activeFns))
			fnResources.Set(float64(activeFns))
		}
	}()
	
	// Application info
	versionHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		bs, err := json.Marshal(struct {
			Name      string
			BuildTime string
		}{
			Name:      "simfaas",
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
	
	// Mock the function lookup
	lookupHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("{}"))
	})
	
	// Mock the pre-warming endpoint
	prewarmHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if fn := r.URL.Query().Get("fn"); useColdStarts && len(fn) > 0 {
			log.Printf("Prewarmed fn %s", fn)
			fnCache.Store(fn, Function{
				name:       fn,
				deployedAt: time.Now().Add(*coldStartDuration),
				lastExec:   time.Now().Add(*coldStartDuration),
			})
			go func() {
				time.Sleep(*coldStartDuration)
				fnActive.Inc()
			}()
		}
		w.WriteHeader(http.StatusOK)
	})
	
	// Mock function resolver endpoint
	resolverHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte("sleep"))
	})
	
	// Mock the actual function execution endpoint
	fnSleepHandler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		var runtime float64 // in seconds
		var err error
		if queryRuntime := r.URL.Query().Get("runtime"); len(queryRuntime) > 0 {
			// Read query
			runtime, err = strconv.ParseFloat(queryRuntime, 64)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
		} else {
			// Read body
			defer r.Body.Close()
			d, err := ioutil.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			var payload SleepPayload
			err = json.Unmarshal(d, &payload)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			runtime = payload.Runtime
		}
		
		// Decide on cold start
		fn := r.URL.Query().Get("fn")
		coldStart, err := time.ParseDuration(r.URL.Query().Get("coldstart.duration"))
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		// First check if cold start was set for the specific request
		if useColdStarts && len(fn) > 0 {
			i, ok := fnCache.LoadOrStore(fn, Function{
				deployedAt: time.Now().Add(*coldStartDuration),
				lastExec:   time.Now().Add(*coldStartDuration),
			})
			if !ok {
				// Simulate deployment
				coldStart = *coldStartDuration
				time.Sleep(coldStart)
				fnActive.Inc()
				log.Printf("Deployed fn %s (cold start: %s)", fn, coldStart.String())
			} else {
				entry := i.(Function)
				// Function is still being deployed, we need to wait a bit longer
				if entry.deployedAt.After(time.Now()) {
					time.Sleep(entry.deployedAt.Sub(time.Now()))
				}
				
				// Refresh timer on the existing function instance
				fnCache.Store(fn, Function{
					name:       fn,
					deployedAt: i.(Function).deployedAt,
					lastExec:   time.Now(),
				})
			}
		} else {
			// If we are not using cold starts we have to record the 'instant' deployment still
			fnActive.Inc()
		}
		
		// Simulate runtime
		du := time.Duration(runtime * float64(time.Second))
		time.Sleep(du)
		result, _ := json.Marshal(map[string]interface{}{
			"start":     start.UnixNano(),
			"coldStart": coldStart.Nanoseconds(),
			"runtime":   du.Nanoseconds(),
			"end":       time.Now().UnixNano(),
		})
		w.WriteHeader(http.StatusOK)
		w.Write(result)
	})
	
	// Collect and expose metrics
	mux.Handle("/metrics", promhttp.Handler())
	instrumentEndpoint(mux, "/", versionHandler)
	instrumentEndpoint(mux, "/v2/functions/sleep", lookupHandler)
	instrumentEndpoint(mux, "/v2/tapService", prewarmHandler)
	instrumentEndpoint(mux, "/v2/getServiceForFunction", resolverHandler)
	instrumentEndpoint(mux, "/fission-function/sleep", fnSleepHandler)
	
	// Start serving
	log.Printf("Serving at %s", *addr)
	log.Fatal((&http.Server{
		Addr:        *addr,
		ReadTimeout: 5 * time.Second,
		Handler:     mux,
	}).ListenAndServe())
}

func instrumentEndpoint(mux *http.ServeMux, path string, handler http.Handler) {
	mux.Handle(path, promhttp.InstrumentHandlerCounter(requestCount.MustCurryWith(prometheus.Labels{"path": path}),
		promhttp.InstrumentHandlerDuration(requestDuration.MustCurryWith(prometheus.Labels{"path": path}),
			promhttp.InstrumentHandlerInFlight(requestInFlight.With(prometheus.Labels{"path": path}),
				handler))))
}

type SleepPayload struct {
	Runtime float64 `json:"runtime"` // in seconds (e.g. 1.043)
}
