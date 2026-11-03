package main

import "testing"

func TestValidate(t *testing.T) {
	good := options{Impl: "goarena", N: 10, ValueSize: 8, Goroutines: 1, ReadRatio: 0.5,
		Dist: "uniform", SampleEvery: 16, Shards: 4, ChunkMiB: 1}
	if err := good.validate(); err != nil {
		t.Fatal(err)
	}
	mutations := []func(o *options){
		func(o *options) { o.Impl = "nope" },
		func(o *options) { o.N = 0 },
		func(o *options) { o.ValueSize = 4 },
		func(o *options) { o.Goroutines = 0 },
		func(o *options) { o.ReadRatio = 2 },
		func(o *options) { o.Dist = "normal" },
		func(o *options) { o.SampleEvery = 3 },
		func(o *options) { o.Shards = 6 },
		func(o *options) { o.ChunkMiB = 0 },
	}
	for i, m := range mutations {
		o := good
		m(&o)
		if o.validate() == nil {
			t.Errorf("mutation %d accepted", i)
		}
	}
}
