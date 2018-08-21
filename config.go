package server

import (
	"runtime"
	"time"

	"github.com/k0kubun/pp"
	"github.com/rai-project/config"
	"github.com/rai-project/vipertags"
)

type serverConfig struct {
	ClientAppName                    string        `json:"client_app_name" config:"client.app_name" default:"rai"`
	ClientUploadBucketName           string        `json:"upload_bucket" config:"client.upload_bucket" default:"files.rai-project.com"`
	ClientUploadDestinationDirectory string        `json:"upload_destination_directory" config:"client.upload_destination_directory" default:"userdata"`
	ClientJobQueueName               string        `json:"job_queue_name" config:"client.job_queue_name"`
	ClientJobTimeLimit               time.Duration `json:"client_job_time_limit" config:"client.job_time_limit"`
  DisableRAIDockerNamespaceProtection bool       `json:"disable_rai_docker_namespace_protection" config:"server.disable_rai_docker_namespace_protection" default:"FALSE"`
	done                             chan struct{} `json:"-" config:"-"`
}

var (
	Config = &serverConfig{
		done: make(chan struct{}),
	}
)

func (serverConfig) ConfigName() string {
	return "Server"
}

func (a *serverConfig) SetDefaults() {
	vipertags.SetDefaults(a)
}

func (a *serverConfig) Read() {
	defer close(a.done)
	vipertags.Fill(a)
	if a.ClientJobQueueName == "" || a.ClientJobQueueName == "default" {
		a.ClientJobQueueName = config.App.Name + "_" + runtime.GOARCH
	}
}

func (c serverConfig) Wait() {
	<-c.done
}

func (c serverConfig) String() string {
	return pp.Sprintln(c)
}

func (c serverConfig) Debug() {
	log.Debug("Server Config = ", c)
}

func init() {
	config.Register(Config)
}
