package gcpace_test

import (
	"fmt"
	"time"

	"github.com/vladislavprovich/goarena/gcpace"
)

func ExampleStart() {
	// A program that keeps 2 GiB of data off-heap and runs in a 3 GiB
	// container: GC frequency as if the 2 GiB were heap, Go memory
	// limited to 1 GiB.
	p := gcpace.Start(gcpace.Config{GOGC: 100, MemoryLimit: 3 << 30, Interval: time.Hour})
	defer p.Stop() // restores the previous GOGC and GOMEMLIMIT
	p.Add(func() int64 { return 2 << 30 })
	p.Update()
	fmt.Println(p.Stats().GoMemoryLimit >> 20)
	// Output: 1024
}
