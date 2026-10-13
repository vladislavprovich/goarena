package gcpace

import "sync"

var (
	defMu     sync.Mutex
	defCfg    Config
	defPacer  *Pacer
	defActive int
)

// SetDefaultConfig sets the configuration used when the default pacer is
// started by the next [Register] (it does not affect a running pacer).
func SetDefaultConfig(cfg Config) {
	defMu.Lock()
	defCfg = cfg
	defMu.Unlock()
}

// Register adds an off-heap source to the process-wide default pacer,
// starting the pacer on the first registration. The returned function
// removes the source; when the last source is removed the pacer stops and
// restores the original GC settings.
func Register(src func() int64) (unregister func()) {
	defMu.Lock()
	if defPacer == nil {
		defPacer = Start(defCfg)
	}
	p := defPacer
	remove := p.Add(src)
	defActive++
	defMu.Unlock()
	p.Update()
	var once sync.Once
	return func() {
		once.Do(func() {
			remove()
			defMu.Lock()
			defActive--
			if defActive == 0 && defPacer == p {
				defPacer = nil
				defMu.Unlock()
				p.Stop()
				return
			}
			defMu.Unlock()
		})
	}
}

// Default returns the running default pacer, or nil.
func Default() *Pacer {
	defMu.Lock()
	defer defMu.Unlock()
	return defPacer
}
