package server

import (
	"github.com/k0kubun/pp"
	"github.com/rai-project/config"
	"github.com/rai-project/vipertags"
)

type serverConfig struct {
	ClientAppName                    string        `json:"client_app_name" config:"client.app_name" default:"rai"`
	ClientUploadBucketName           string        `json:"upload_bucket" config:"client.upload_bucket" default:"files.rai-project.com"`
	ClientUploadDestinationDirectory string        `json:"upload_destination_directory" config:"client.upload_destination_directory" default:"userdata"`
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
	vipertags.Fill(a)
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
