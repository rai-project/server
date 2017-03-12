package server

import (
	"github.com/fatih/color"
	"github.com/rai-project/config"
	"github.com/rai-project/model"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/pubsub/redis"
)

type WorkRequest struct {
	model.JobRequest
	publisher      pubsub.Publisher
	publishChannel string
	pubsubConn     pubsub.Connection
}

func NewWorkerRequest(job model.JobRequest) (*WorkRequest, error) {
	conn, err := redis.New()
	if err != nil {
		return nil, err
	}
	pub, err := redis.NewPublisher(conn)
	if err != nil {
		return nil, err
	}

	return &WorkRequest{
		JobRequest:     job,
		pubsubConn:     conn,
		publishChannel: config.App.Name + "/log-" + job.ID,
		publisher:      pub,
	}, nil
}

func (w *WorkRequest) Publish(i interface{}) error {
	return w.publisher.Publish(w.publishChannel, i)
}

func (w *WorkRequest) Start() error {
	w.Publish(color.YellowString("✱ Server has accepted your job submission."))
	return nil
}

func (w *WorkRequest) Stop() error {

	w.publisher.End(w.publishChannel)

	if err := w.publisher.Stop(); err != nil {
		log.WithError(err).Error("failed to stop pubsub publisher")
	}

	if err := w.pubsubConn.Close(); err != nil {
		log.WithError(err).Error("failed to close pubsub connection")
	}

	return nil
}
