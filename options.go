package server

import (
	"context"
	"io"
)

type Options struct {
	stdout                           io.WriteCloser
	stderr                           io.WriteCloser
	jobQueueName                     string
	numworkers                       int
	gpuaffinity                      int
	containerBuildDirectory          string
	containerSourceDirectory         string
	context                          context.Context
	clientUploadBucketName           string
	clientUploadDestinationDirectory string
	clientAppName                    string
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
		o.numworkers = n
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
