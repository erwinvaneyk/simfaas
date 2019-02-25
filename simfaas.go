package simfaas

import (
	`context`
	`errors`
	`go.uber.org/atomic`
	`log`
	`sync`
	`time`
)

const (
	functionGCInterval = time.Second
)

var (
	ErrFunctionNotFound = errors.New("function does not exist")
)

type FunctionConfig struct {
	ColdStartDuration time.Duration
	KeepWarmDuration  time.Duration
	Runtime           time.Duration
	
	// instanceCapacity defines the number of parallel executions a function instance can handle
	// Negative or zero indicates an infinite capacity; no more than 1 instance is used.
	// instanceCapacity  int
}

type ExecutionReport struct {
	ColdStart  time.Duration
	Runtime    time.Duration
	StartedAt  time.Time
	FinishedAt time.Time
}

type Function struct {
	*FunctionConfig
	name       string
	deployedAt time.Time
	lastExec   time.Time
	instances  atomic.Uint32
	active     atomic.Uint32
	mu         sync.RWMutex
}

type Platform struct {
	functions *sync.Map // map[string]*Function
	init      *sync.Once
	stopFn    func()
	activeFns atomic.Uint32
}

func New() *Platform {
	return &Platform{
		functions: &sync.Map{},
		init: &sync.Once{},
	}
}

func (p *Platform) Start() error {
	p.init.Do(func() {
		ctx, closeFn := context.WithCancel(context.Background())
		go p.runFunctionGC(ctx.Done())
		p.stopFn = closeFn
	})
	return nil
}

// Future: change to priority queue
func (p *Platform) runFunctionGC(closeC <-chan struct{}) {
	ticker := time.NewTicker(functionGCInterval)
	for {
		select {
		case <-closeC:
			return
		case <-ticker.C:
		}
		now := time.Now()
		p.RangeFunctions(func(k string, fn *Function) bool {
			if fn.instances.Load() > 0 &&
				fn.lastExec.Add(fn.KeepWarmDuration).Before(now) &&
				fn.deployedAt.Add(fn.KeepWarmDuration).Before(now) &&
				fn.active.Load() == 0 {
				p.cleanup(fn)
				log.Printf("%s: cleaned up instance (1 -> 0)", k)
			}
			return true
		})
	}
}

func (p *Platform) RangeFunctions(rangeFn func(k string, fn *Function) bool) {
	p.functions.Range(func(key, value interface{}) bool {
		return rangeFn(key.(string), value.(*Function))
	})
}

func (p *Platform) ActiveExecutions() uint32 {
	return p.activeFns.Load()
}

func (p *Platform) Close() error {
	p.stopFn()
	return nil
}

func (p *Platform) Define(fnName string, config *FunctionConfig) {
	p.functions.Store(fnName, &Function{
		name:           fnName,
		FunctionConfig: config,
	})
}

func (p *Platform) cleanup(fn *Function) {
	fn.instances.Store(0)
}

// Run emulates a function execution in a synchronous way, sleeping for the entire executionRuntime
//
// TODO handle inputs and outputs
func (p *Platform) Run(fnName string, executionRuntime *time.Duration) (*ExecutionReport, error) {
	startedAt := time.Now()
	// Find the function
	fn, ok := p.Get(fnName)
	if !ok {
		return nil, ErrFunctionNotFound
	}
	
	// Ensure that there is enough capacity
	var coldStart time.Duration
	if fn.instances.Load() == 0 {
		coldStart = p.deploy(fn)
	}
	
	// Simulate function execution
	fn.active.Inc()
	p.activeFns.Inc()
	runtime := fn.Runtime
	if executionRuntime != nil {
		runtime = *executionRuntime
	}
	time.Sleep(runtime)
	fn.active.Dec()
	p.activeFns.Dec()
	finishedAt := time.Now()
	
	// Update function stats
	fn.mu.Lock()
	fn.lastExec = time.Now()
	fn.mu.Unlock()
	return &ExecutionReport{
		StartedAt:  startedAt,
		FinishedAt: finishedAt,
		Runtime:	finishedAt.Sub(startedAt),
		ColdStart:  coldStart,
	}, nil
}

func (p *Platform) Get(fnName string) (*Function, bool) {
	v, ok := p.functions.Load(fnName)
	if !ok {
		return nil, ok
	}
	return v.(*Function), ok
}

func (p *Platform) Deploy(fnName string) (coldStart time.Duration, err error) {
	// Find the function
	fn, ok := p.Get(fnName)
	if !ok {
		return 0, ErrFunctionNotFound
	}
	
	return p.deploy(fn), nil
}

func (p *Platform) deploy(fn *Function) (coldStart time.Duration) {
	// Deploy if there is no instance available
	startedAt := time.Now()
	fn.mu.Lock()
	if fn.instances.Load() == 0 {
		time.Sleep(fn.ColdStartDuration)
		fn.instances.Store(1)
		fn.deployedAt = time.Now()
		log.Printf("%s: deployed instance (0 -> 1)", fn.name)
	}
	fn.mu.Unlock()
	return time.Now().Sub(startedAt)
}
