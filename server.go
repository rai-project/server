package server

import (
	"context"
	"errors"
	"io"
	"runtime"

	"github.com/aws/aws-sdk-go/aws/session"
	colorable "github.com/mattn/go-colorable"
	"github.com/rai-project/auth"
	"github.com/rai-project/aws"
	"github.com/rai-project/broker"
	"github.com/rai-project/broker/rabbitmq"
	"github.com/rai-project/broker/sqs"
	"github.com/rai-project/config"
	"github.com/rai-project/docker"
	"github.com/rai-project/model"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/serializer"
	"github.com/rai-project/serializer/json"
	"github.com/rai-project/uuid"
)

type Server struct {
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

	// BeforeShutdown is an optional callback function that is called
	// before the server is closed.
	BeforeShutdown func()

	// AfterShutdown is an optional callback function that is called
	// after the server is closed.
	AfterShutdown func()

	available_workers int
}

type nopWriterCloser struct {
	io.Writer
}

func (nopWriterCloser) Close() error { return nil }

func New(opts ...Option) (*Server, error) {
	nworkers := Config.NumberOfWorkers

	stdout, stderr := colorable.NewColorableStdout(), colorable.NewColorableStderr()
	if !config.App.Color {
		stdout = colorable.NewNonColorable(stdout)
		stderr = colorable.NewNonColorable(stderr)
	}

	options := Options{
		stdout:                           nopWriterCloser{stdout},
		stderr:                           nopWriterCloser{stderr},
		numworkers:                       nworkers,
		jobQueueName:                     Config.ClientJobQueueName,
		containerBuildDirectory:          DefaultContainerBuildDirectory,
		containerSourceDirectory:         DefaultContainerSourceDirectory,
		clientUploadBucketName:           Config.ClientUploadBucketName,
		clientUploadDestinationDirectory: Config.ClientUploadDestinationDirectory,
		clientAppName:                    Config.ClientAppName,
		context:                          context.Background(),
		timelimit:                        Config.ClientJobTimeLimit,
	}

	for _, o := range opts {
		o(&options)
	}

	id := uuid.NewV4()

	// Create an AWS session
	// we put minio login info under aws on s390x
	awsSession, err := aws.NewSession(
		aws.Region(aws.AWSRegionUSEast1),
		aws.AccessKey(aws.Config.AccessKey),
		aws.SecretKey(aws.Config.SecretKey),
	)
	if err != nil {
		return nil, err
	}

	go docker.PeriodicCleanupDeadContainers()

	return &Server{
		ID:          id,
		isConnected: false,
		options:     options,
		awsSession:  awsSession,
		serializer:  json.New(),
	}, nil
}

func (s *Server) jobHandler(pub broker.Publication) error {
	jobRequest := new(model.JobRequest)

	msg := pub.Message()
	if msg == nil {
		return errors.New("received a nil message")
	}
	body := msg.Body
	err := s.serializer.Unmarshal(body, jobRequest)
	if err != nil {
		log.WithError(err).WithField("id", msg.ID).Error("failed to parse job request")
		return err
	}

	if jobRequest.PushQ() {
		buildImage := jobRequest.BuildSpecification.Commands.BuildImage
		push := buildImage.Push
		if push.ImageName == "" {
			push.ImageName = buildImage.ImageName
		}
	}

	work, err := NewWorkerRequest(jobRequest, s.options, &s.available_workers)
	if err != nil {
		return err
	}
	s.dispatcher.workQueue <- work

	return nil
}

func (s *Server) publishSubscribe() error {
	log.Debug("Server subscribed to ", s.options.jobQueueName, " queue")
	var brkr broker.Broker
	var err error
	if runtime.GOARCH == "s390x" {
		brkr = rabbitmq.New(
			rabbitmq.QueueName(s.options.jobQueueName),
			broker.Serializer(json.New()),
		)
	} else {
		brkr, err = sqs.New(
			sqs.QueueName(s.options.jobQueueName),
			broker.Serializer(json.New()),
			sqs.Session(s.awsSession),
			sqs.AvailableWorkers(&s.available_workers),
		)
		if err != nil {
			return err
		}
	}
	brkr.Connect()
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

func (s *Server) Connect() error {
	s.dispatcher = StartDispatcher(s.options.numworkers)

	//Set Number of Workers
	s.available_workers = s.options.numworkers

	if err := s.publishSubscribe(); err != nil {
		return err
	}

	if err := s.broker.Connect(); err != nil {
		return err
	}
	s.isConnected = true
	return nil
}

func (s *Server) Disconnect() error {
	if !s.isConnected {
		return nil
	}

	log.Debug("shutting down server")

	defer func() {
		if s.AfterShutdown != nil {
			s.AfterShutdown()
		}
		s.isConnected = false
	}()

	if s.BeforeShutdown != nil {
		s.BeforeShutdown()
	}

	for k, pub := range s.publishers {
		pub.End(k)
	}

	s.jobSubscriber.Unsubscribe()

	return s.broker.Disconnect()
}

func (s *Server) Close() error {
	return s.Disconnect()
}
