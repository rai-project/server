package server

import (
	"io"

	"github.com/aws/aws-sdk-go/aws/session"
	"github.com/k0kubun/pp"
	colorable "github.com/mattn/go-colorable"
	"github.com/rai-project/auth"
	"github.com/rai-project/aws"
	"github.com/rai-project/broker"
	"github.com/rai-project/broker/sqs"
	"github.com/rai-project/config"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/serializer/json"
	"github.com/rai-project/uuid"
	"github.com/spf13/viper"
)

type server struct {
	ID            string
	awsSession    *session.Session
	options       Options
	broker        broker.Broker
	pubsubConn    pubsub.Connection
	profile       auth.Profile
	isConnected   bool
	jobSubscriber broker.Subscriber
	publishers    map[string]pubsub.Publisher
}

type nopWriterCloser struct {
	io.Writer
}

func (nopWriterCloser) Close() error { return nil }

func New(opts ...Option) (*server, error) {
	stdout, stderr := colorable.NewColorableStdout(), colorable.NewColorableStderr()
	if viper.GetBool("app.color") {
		stdout = colorable.NewNonColorable(stdout)
		stderr = colorable.NewNonColorable(stderr)
	}
	options := Options{
		stdout: nopWriterCloser{stdout},
		stderr: nopWriterCloser{stderr},
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
	}, nil
}

func (s *server) jobHandler(pub broker.Publication) error {
	msg := pub.Message()

	pp.Println("id = ", msg.ID, " body = ", string(msg.Body))
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
