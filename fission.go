package simfaas

import (
	`encoding/json`
	`errors`
	`io/ioutil`
	`log`
	`net/http`
	`net/url`
	`regexp`
	`strconv`
	`strings`
	`time`
)

// Fission is a wrapper on top of simfaas that emulates a part of the
// interface of Fission.
type Fission struct {
	Platform                 *Platform
	FnFactory                func(name string) *FunctionConfig
	
	// CreateUndefinedFunctions enables, if set to true,
	// the automatic creation of a function if it is called.
	CreateUndefinedFunctions bool
}

func (f *Fission) Start() error {
	return f.Platform.Start()
}

func (f *Fission) Close() error {
	return f.Platform.Close()
}

// GetServiceForFunction emulates the mapping of a function to a service
// name/host. Currently it just returns the name of the function as the
// service name.
func (f *Fission) GetServiceForFunction(fnName string) (string, error) {
	f.createIfUndefined(fnName)
	fn, ok := f.Platform.Get(fnName)
	if !ok {
		return "", ErrFunctionNotFound
	}
	return fn.name, nil
}

// TapService deploys (or keeps deployed) a function instance for the function.
//
// Note: similar as in GetServiceForFunction, we assume that the service url is
// just the function name.
func (f *Fission) TapService(svcURL string) error {
	if len(svcURL) == 0 {
		return errors.New("no url provided to tap")
	}
	fnName := svc2fn(svcURL)
	f.createIfUndefined(fnName)
	
	fn, ok := f.Platform.Get(fnName)
	if !ok {
		return ErrFunctionNotFound
	}
	
	// Tapping is an async operation
	go func() {
		_ = f.Platform.deploy(fn)
	}()
	return nil
}

// Run emulates the execution of a Fission Function.
//
// If the runtime is not nil it will be used to override the runtime
// specified in the config of the function.
func (f *Fission) Run(fnName string, runtime *time.Duration) (*ExecutionReport, error) {
	f.createIfUndefined(fnName)
	return f.Platform.Run(fnName, runtime)
}

func (f *Fission) Serve() http.Handler {
	handler := &RegexpHandler{}
	handler.HandleFunc(regexp.MustCompile("/v2/functions/.*"), f.HandleFunctionsGet)
	handler.HandleFunc(regexp.MustCompile("/v2/tapService"), f.HandleTapService)
	handler.HandleFunc(regexp.MustCompile("/v2/getServiceForFunction"), f.HandleGetServiceForFunction)
	handler.HandleFunc(regexp.MustCompile("/fission-function/.*"), f.HandleFunctionRun)
	return handler
}

// HandleGetServiceForFunction emulates the /v2/getServiceForFunction
// Fission endpoint.
func (f *Fission) HandleGetServiceForFunction(w http.ResponseWriter, r *http.Request) {
	bs, err := ioutil.ReadAll(r.Body)
	r.Body.Close()
	if err != nil {
		http.Error(w, "failed to read function metadata", 400)
		return
	}
	meta := &ObjectMeta{}
	err = json.Unmarshal(bs, meta)
	if err != nil {
		http.Error(w, "failed to parse function metadata", 400)
		return
	}
	
	svc, err := f.GetServiceForFunction(meta.Name)
	if err != nil {
		http.Error(w, err.Error(), 404)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write([]byte(svc))
}

// HandleTapService emulates the /v2/tapService Fission endpoint.
func (f *Fission) HandleTapService(w http.ResponseWriter, r *http.Request) {
	bs, err := ioutil.ReadAll(r.Body)
	defer r.Body.Close()
	if err != nil {
		log.Printf("Failed to parse service to tap: %v", err)
		http.Error(w, "failed to parse service to tap", 400)
		return
	}
	svcURL := string(bs)
	err = f.TapService(svcURL)
	if err != nil {
		log.Printf("%s: failed to prewarm: %v", svcURL, err)
		http.Error(w, "failed to parse service to tap", 400)
		return
	}
	log.Printf("%s: prewarmed instance", svcURL)
	w.WriteHeader(http.StatusOK)
}

// HandleFunctionsGet emulates the /v2/functions/.* Fission endpoints.
// Currently it simply returns an empty map.
func (f *Fission) HandleFunctionsGet(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// HandleFunctionRun emulates the /fission-function/.* Fission endpoints.
//
// It checks for the presence of the runtime query parameter,
// which allows you to override the runtime of the function.
func (f *Fission) HandleFunctionRun(w http.ResponseWriter, r *http.Request) {
	// Parse arguments: fnname, runtime
	var seconds float64
	var err error
	if queryRuntime := r.URL.Query().Get("runtime"); len(queryRuntime) > 0 {
		seconds, err = strconv.ParseFloat(queryRuntime, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	runtime := time.Duration(seconds * float64(time.Second))
	fnName := getFunctionNameFromUrl(r.URL)
	
	report, err := f.Run(fnName, &runtime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	result, err := json.Marshal(report)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	w.WriteHeader(http.StatusOK)
	w.Write(result)
}

func (f *Fission) createIfUndefined(fnName string) {
	// Create function in simulator if undefined
	if f.CreateUndefinedFunctions {
		if _, ok := f.Platform.Get(fnName); !ok {
			fnCfg := f.FnFactory(fnName)
			f.Platform.Define(fnName, fnCfg)
			log.Printf("Created new function %s with config: %+v", fnName, fnCfg)
		}
	}
}

type ObjectMeta struct {
	Name string `json:"name,omitempty" protobuf:"bytes,1,opt,name=name"`
	// Namespace string `json:"namespace,omitempty" protobuf:"bytes,3,opt,name=namespace"`
}

func getFunctionNameFromUrl(url *url.URL) string {
	return url.Path[strings.LastIndex(url.Path, "/")+1:]
}

func svc2fn(svc string) string {
	svcURL, err := url.Parse(svc)
	if err != nil {
		return svc
	}
	return svcURL.Host
}
