package server

import (
	"io"

	"github.com/fatih/color"
	"github.com/rai-project/config"
	"github.com/rai-project/docker"
	"github.com/rai-project/model"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/pubsub/redis"
)

type WorkRequest struct {
	model.JobRequest
	publisher      pubsub.Publisher
	publishChannel string
	pubsubConn     pubsub.Connection
	docker         *docker.Client
	container      *docker.Container
	buildSpec      model.BuildSpecification
	stdout         io.Writer
	stderr         io.Writer
}

type publishWriter struct {
	publisher      pubsub.Publisher
	publishChannel string
	kind           model.ResponseKind
}

func (w *publishWriter) Write(p []byte) (int, error) {
	w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind: w.kind,
		Body: p,
	})
	return len(p), nil
}

func NewWorkerRequest(job model.JobRequest) (*WorkRequest, error) {
	publishChannel := config.App.Name + "/log-" + job.ID

	conn, err := redis.New()
	if err != nil {
		return nil, err
	}
	publisher, err := redis.NewPublisher(conn)
	if err != nil {
		return nil, err
	}

	stdout := &publishWriter{
		publisher:      publisher,
		publishChannel: publishChannel,
		kind:           model.StdoutResponse,
	}

	stderr := &publishWriter{
		publisher:      publisher,
		publishChannel: publishChannel,
		kind:           model.StderrResponse,
	}

	d, err := docker.NewClient(
		docker.Stdout(stdout),
		docker.Stderr(stderr),
	)

	return &WorkRequest{
		JobRequest:     job,
		pubsubConn:     conn,
		publishChannel: publishChannel,
		publisher:      publisher,
		docker:         d,
		buildSpec:      job.BuildSpecification,
	}, nil
}

func (w *WorkRequest) publishStdout(s string) error {
	return w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind: model.StdoutResponse,
		Body: []byte(s),
	})
}

func (w *WorkRequest) publishStderr(s string) error {
	return w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind: model.StderrResponse,
		Body: []byte(s),
	})
}

func (w *WorkRequest) Start() error {
	buildSpec := w.buildSpec

	w.publishStdout(color.YellowString("✱ Server has accepted your job submission and started to configure the container."))

	imageName := buildSpec.RAI.ContainerImage
	w.publishStdout(color.YellowString("✱ Using " + imageName + " as container image."))

	if !w.docker.HasImage(imageName) {
		log.WithField("id", w.ID).WithField("image", imageName).Debug("image not found")
		err := w.docker.PullImage(imageName)
		if err != nil {
			w.publishStderr(color.RedString("✱ Unable to pull " + imageName + " from docker hub repository."))
			log.WithError(err).WithField("image", imageName).Error("unable to pull image")
			return err
		}
	}
	containerOpts := []docker.ContainerOption{}
	if buildSpec.Resources.GPUs >= 1 {
		if buildSpec.Resources.GPUs > 1 {
			w.publishStderr(color.RedString("✱ Only one gpu can be currently supported for a job submission."))
			buildSpec.Resources.GPUs = 1
		}
		containerOpts = append(containerOpts, docker.CUDADevice(0))
	}
	container, err := docker.NewContainer(w.docker, containerOpts...)
	if err != nil {
		w.publishStderr(color.RedString("✱ Unable to create " + imageName + " container."))
		log.WithError(err).WithField("image", imageName).Error("unable to create container")
		return err
	}
	w.container = container

	w.publishStdout(color.YellowString("✱ Starting container."))

	if err := container.Start(); err != nil {
		w.publishStderr(color.RedString("✱ Unable to start " + imageName + " container."))
		log.WithError(err).WithField("image", imageName).Error("unable to start container")
		return err
	}

	for _, cmd := range buildSpec.Commands.Build {
		exec, err := docker.NewExecutionFromString(container, cmd)
		if err != nil {
			w.publishStderr(color.RedString("✱ Unable create run command " + cmd + ". Make sure that the input is a valid shell command."))
			log.WithError(err).WithField("cmd", cmd).Error("unable to create docker execution")
			return err
		}
		exec.Stdin = nil
		exec.Stderr = w.stderr
		exec.Stdout = w.stdout

		w.publishStdout(color.GreenString("✱ Running " + cmd))

		if err := exec.Run(); err != nil {
			w.publishStderr(color.RedString("✱ Unable to start running " + cmd + ". Make sure that the input is a valid shell command."))
			log.WithError(err).WithField("cmd", cmd).Error("unable to create docker execution")
			return err
		}
	}
	log.WithField("id", w.ID).WithField("image", imageName).Debug("finished ")

	return nil
}

func (w *WorkRequest) Stop() error {

	if w.container != nil {
		w.container.Stop()
	}

	if w.docker != nil {
		w.docker.Close()
	}

	if w.publisher != nil {
		w.publishStdout(color.GreenString("✱ Server has ended your request."))
		w.publisher.End(w.publishChannel)

		if err := w.publisher.Stop(); err != nil {
			log.WithError(err).Error("failed to stop pubsub publisher")
		}
	}

	if w.pubsubConn != nil {
		if err := w.pubsubConn.Close(); err != nil {
			log.WithError(err).Error("failed to close pubsub connection")
		}
	}

	return nil
}
