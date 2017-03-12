package server

import (
	"errors"
	"io"
	"runtime"

	"github.com/aws/aws-sdk-go/aws/session"
	colorable "github.com/mattn/go-colorable"
	"github.com/rai-project/auth"
	"github.com/rai-project/aws"
	"github.com/rai-project/broker"
	"github.com/rai-project/broker/sqs"
	"github.com/rai-project/config"
	"github.com/rai-project/model"
	nvidiasmi "github.com/rai-project/nvidia-smi"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/serializer"
	"github.com/rai-project/serializer/json"
	"github.com/rai-project/uuid"
)

type server struct {
	ID            string
	awsSession    *session.Session
	options       Options
	broker        broker.Broker
	pubsubConn    pubsub.Connection
	profile       auth.Profile
	isConnected   bool
	serializer    serializer.Serializer
	jobSubscriber broker.Subscriber
	dispatcher    *Dispatcher
	publishers    map[string]pubsub.Publisher
}

type nopWriterCloser struct {
	io.Writer
}

func (nopWriterCloser) Close() error { return nil }

func New(opts ...Option) (*server, error) {
	nworkers := runtime.NumCPU()
	if nvidiasmi.HasGPU {
		nworkers = nvidiasmi.GPUCount
	}
	if config.IsDebug {
		nworkers = 1
	}

	stdout, stderr := colorable.NewColorableStdout(), colorable.NewColorableStderr()
	if !config.App.Color {
		stdout = colorable.NewNonColorable(stdout)
		stderr = colorable.NewNonColorable(stderr)
	}

	options := Options{
		stdout:     nopWriterCloser{stdout},
		stderr:     nopWriterCloser{stderr},
		numworkers: nworkers,
	}

	for _, o := range opts {
		o(&options)
	}

	id := uuid.NewV4()

	// Create an AWS session
	awsSession, err := aws.NewSession(
		aws.Region(aws.AWSRegionUSEast1),
		aws.EncryptedAccessKey(aws.Config.AccessKey),
		aws.EncryptedSecretKey(aws.Config.SecretKey),
		aws.Sts(id),
	)
	if err != nil {
		return nil, err
	}

	return &server{
		ID:          id,
		isConnected: false,
		options:     options,
		awsSession:  awsSession,
		serializer:  json.New(),
	}, nil
}

func (s *server) jobHandler(pub broker.Publication) error {
	var jobRequest model.JobRequest

	msg := pub.Message()
	if msg == nil {
		return errors.New("recieved a nil message")
	}
	body := msg.Body
	err := s.serializer.Unmarshal(body, &jobRequest)
	if err != nil {
		log.WithError(err).WithField("id", msg.ID).Error("failed to parse job request")
		return err
	}

	work, err := NewWorkerRequest(jobRequest)
	if err != nil {
		return err
	}
	s.dispatcher.workQueue <- work

	return nil
}

func (s *server) publishSubscribe() error {
	brkr, err := sqs.New(
		sqs.QueueName(config.App.Name),
		broker.Serializer(json.New()),
		sqs.Session(s.awsSession),
	)
	if err != nil {
		return err
	}

	subscriber, err := brkr.Subscribe(
		"rai",
		s.jobHandler,
		broker.AutoAck(true),
	)
	if err != nil {
		return err
	}

	s.jobSubscriber = subscriber
	s.broker = brkr

	return nil
}

func (s *server) Connect() error {
	s.dispatcher = StartDispatcher(s.options.numworkers)

	if err := s.publishSubscribe(); err != nil {
		return err
	}

	if err := s.broker.Connect(); err != nil {
		return err
	}
	s.isConnected = true
	return nil
}

func (s *server) Disconnect() error {
	if !s.isConnected {
		return nil
	}

	for k, pub := range s.publishers {
		pub.End(k)
	}

	s.jobSubscriber.Unsubscribe()

	return s.broker.Disconnect()
}
