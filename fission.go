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

type Fission struct {
	Platform                 *Platform
	FnFactory                func(name string) *FunctionConfig
	CreateUndefinedFunctions bool
}

func (f *Fission) GetServiceForFunction(fnName string) (string, error) {
	f.createIfUndefined(fnName)
	fn, ok := f.Platform.Get(fnName)
	if !ok {
		return "", ErrFunctionNotFound
	}
	return fn.name, nil
}

// We assume that url is just the function name
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

func (f *Fission) Serve() http.Handler {
	handler := &RegexpHandler{}
	handler.HandleFunc(regexp.MustCompile("/v2/functions/.*"), f.HandleFunctionsGet)
	handler.HandleFunc(regexp.MustCompile("/v2/tapService"), f.HandleTapService)
	handler.HandleFunc(regexp.MustCompile("/v2/getServiceForFunction"), f.HandleGetServiceForFunction)
	handler.HandleFunc(regexp.MustCompile("/fission-function/.*"), f.HandleFunctionRun)
	return handler
}

// /v2/getServiceForFunction
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

func (f *Fission) Start() error {
	return f.Platform.Start()
}

// /v2/tapService
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

// /v2/functions/.*
func (f *Fission) HandleFunctionsGet(w http.ResponseWriter, r *http.Request) {
	w.WriteHeader(http.StatusOK)
	w.Write([]byte("{}"))
}

// /fission-function/
func (f *Fission) HandleFunctionRun(w http.ResponseWriter, r *http.Request) {
	// Parse arguments: fnname, runtime
	var seconds float64
	var err error
	if queryRuntime := r.URL.Query().Get("runtime"); len(queryRuntime) > 0 {
		// Read query
		seconds, err = strconv.ParseFloat(queryRuntime, 64)
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
	}
	runtime := time.Duration(seconds * float64(time.Second))
	fnName := getFunctionNameFromUrl(r.URL)
	f.createIfUndefined(fnName)
	
	// Run function
	report, err := f.Platform.Run(fnName, &runtime)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	
	// Simulate runtime
	result, _ := json.Marshal(map[string]interface{}{
		"started_at":  report.StartedAt.UnixNano(),
		"finished_at": report.FinishedAt.UnixNano(),
		"coldStart":   report.ColdStart.Nanoseconds(),
		"runtime":     report.Runtime.Nanoseconds(),
	})
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

var fnUrlRegEx = regexp.MustCompile("/(.*)$")

func getFunctionNameFromUrl(url *url.URL) string {
	return url.Path[strings.LastIndex(url.Path, "/")+1:]
}

func svc2fn(svc string) string { return svc }
