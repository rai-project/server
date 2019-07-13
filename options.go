package server

import (
	"context"
	"io"
	"time"
)

type Options struct {
	stdout                           io.WriteCloser
	stderr                           io.WriteCloser
	jobQueueName                     string
	numWorkers                       int64
	gpuaffinity                      int
	containerBuildDirectory          string
	containerSourceDirectory         string
	context                          context.Context
	clientUploadBucketName           string
	clientUploadDestinationDirectory string
	clientAppName                    string
	timelimit                        time.Duration
	onClose                          []func()
	onWorkerClose                    []func()
	IncrementAvailableWorkers        func()
}

type Option func(*Options)

var (
	DefaultContainerBuildDirectory  = "/build"
	DefaultContainerSourceDirectory = "/src"
)

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
		o.numWorkers = int64(n)
	}
}

func GPUAffinity(n int) Option {
	return func(o *Options) {
		o.gpuaffinity = n
	}
}

func JobQueueName(s string) Option {
	return func(o *Options) {
		o.jobQueueName = s
	}
}

func ContainerBuildDirectory(s string) Option {
	return func(o *Options) {
		o.containerBuildDirectory = s
	}
}

func ContainerSourceDirectory(s string) Option {
	return func(o *Options) {
		o.containerSourceDirectory = s
	}
}

func ClientUploadBucketName(s string) Option {
	return func(o *Options) {
		o.clientUploadBucketName = s
	}
}

func ClientUploadDestinationDirectory(s string) Option {
	return func(o *Options) {
		o.clientUploadDestinationDirectory = s
	}
}

func ClientAppName(s string) Option {
	return func(o *Options) {
		o.clientAppName = s
	}
}

func TimeLimit(d time.Duration) Option {
	return func(o *Options) {
		o.timelimit = d
	}
}

// OnClose ...
func OnClose(f func()) Option {
	return func(o *Options) {
		o.onClose = append(o.onClose, f)
	}
}

// OnWorkerClose ...
func OnWorkerClose(f func()) Option {
	return func(o *Options) {
		o.onWorkerClose = append(o.onWorkerClose, f)
	}
}

// Increment Available Workers ...
func IncrementAvailableWorkersSubscribe(f func()) Option {
	return func(o *Options) {
		o.IncrementAvailableWorkers = f
	}
}
