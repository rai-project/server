package server

import "io"

type Options struct {
	stdout     io.WriteCloser
	stderr     io.WriteCloser
	numworkers int
}

type Option func(*Options)

func Stdout(s io.WriteCloser) Option {
	return func(o *Options) {
		o.stdout = s
	}
}

func Stderr(s io.WriteCloser) Option {
	return func(o *Options) {
		o.stderr = s
	}
}

func NumWorkers(n int) Option {
	return func(o *Options) {
		o.numworkers = n
	}
}
