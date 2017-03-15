package server

import (
	"io"
	"time"

	"bytes"

	azaws "github.com/aws/aws-sdk-go/aws"
	"github.com/fatih/color"
	"github.com/rai-project/archive"
	"github.com/rai-project/aws"
	"github.com/rai-project/docker"
	"github.com/rai-project/model"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/pubsub/redis"
	"github.com/rai-project/store"
	"github.com/rai-project/store/s3"
)

type WorkRequest struct {
	model.JobRequest
	publisher      pubsub.Publisher
	publishChannel string
	pubsubConn     pubsub.Connection
	docker         *docker.Client
	container      *docker.Container
	buildSpec      model.BuildSpecification
	store          store.Store
	stdout         io.Writer
	stderr         io.Writer
	serverOptions  Options
}

type publishWriter struct {
	publisher      pubsub.Publisher
	publishChannel string
	kind           model.ResponseKind
}

func (w *publishWriter) Write(p []byte) (int, error) {
	w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind:      w.kind,
		Body:      p,
		CreatedAt: time.Now(),
	})
	return len(p), nil
}

var (
	DefaultUploadExpiration = func() time.Time {
		return time.Now().AddDate(0, 1, 0) // next month
	}
)

func NewWorkerRequest(job model.JobRequest, serverOpts Options) (*WorkRequest, error) {
	publishChannel := serverOpts.clientAppName + "/log-" + job.ID

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

	session, err := aws.NewSession(
		aws.Region(aws.AWSRegionUSEast1),
		aws.AccessKey(aws.Config.AccessKey),
		aws.SecretKey(aws.Config.SecretKey),
		aws.Sts("server-"+job.ID),
	)
	if err != nil {
		return nil, err
	}
	st, err := s3.New(
		s3.Session(session),
		store.Bucket(serverOpts.clientUploadBucketName),
	)
	if err != nil {
		return nil, err
	}

	d, err := docker.NewClient(
		docker.Stdout(stdout),
		docker.Stderr(stderr),
	)
	if err != nil {
		return nil, err
	}

	return &WorkRequest{
		JobRequest:     job,
		pubsubConn:     conn,
		publishChannel: publishChannel,
		publisher:      publisher,
		docker:         d,
		buildSpec:      job.BuildSpecification,
		store:          st,
		serverOptions:  serverOpts,
	}, nil
}

func (w *WorkRequest) publishStdout(s string) error {
	return w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind:      model.StdoutResponse,
		Body:      []byte(s),
		CreatedAt: time.Now(),
	})
}

func (w *WorkRequest) publishStderr(s string) error {
	return w.publisher.Publish(w.publishChannel, model.JobResponse{
		Kind:      model.StderrResponse,
		Body:      []byte(s),
		CreatedAt: time.Now(),
	})
}

func (w *WorkRequest) Start() error {
	buildSpec := w.buildSpec

	defer func() {
		w.publishStdout(color.GreenString("✱ Server has ended your request."))
		w.publisher.End(w.publishChannel)
	}()

	w.publishStdout(color.YellowString("✱ Server has accepted your job submission and started to configure the container."))

	w.publishStdout(color.YellowString("✱ Downloading your code."))

	buf := new(azaws.WriteAtBuffer)
	if err := w.store.DownloadTo(buf, w.UploadKey); err != nil {
		w.publishStdout(color.RedString("✱ Failed to download your code."))
		log.WithError(err).WithField("key", w.UploadKey).Error("failed to download user code from store")
		return err
	}

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

	srcDir := w.serverOptions.containerSourceDirectory
	buildDir := w.serverOptions.containerBuildDirectory

	containerOpts := []docker.ContainerOption{
		docker.AddVolume(srcDir),
		docker.AddVolume(buildDir),
	}
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

	if err := container.CopyToContainer(srcDir, bytes.NewBuffer(buf.Bytes())); err != nil {
		w.publishStderr(color.RedString("✱ Unable to copy your data to the container directory " + srcDir + " ."))
		log.WithError(err).WithField("dir", srcDir).Error("unable to upload user data to container")
		return err
	}

	defer func() {
		opts := w.serverOptions
		dir := opts.containerBuildDirectory
		r, err := container.CopyFromContainer(dir)
		if err != nil {
			w.publishStderr(color.RedString("✱ Unable to copy your output data in " + dir + " from the container."))
			log.WithError(err).WithField("dir", dir).Error("unable to get user data from container")
			return
		}

		uploadKey := opts.clientUploadDestinationDirectory + "/build-" + w.ID + ".tar." + archive.Config.CompressionFormatString
		key, err := w.store.UploadFrom(
			r,
			uploadKey,
			s3.Expiration(DefaultUploadExpiration()),
			s3.Metadata(map[string]interface{}{
				"id":           w.ID,
				"type":         "server_upload",
				"worker":       info(),
				"profile":      w.User,
				"submitted_at": w.CreatedAt,
				"created_at":   time.Now(),
			}),
			s3.ContentType(archive.MimeType()),
		)
		if err != nil {
			w.publishStderr(color.RedString("✱ Unable to upload your output data in " + dir + " to the store server."))
			log.WithError(err).WithField("dir", dir).WithField("key", uploadKey).Error("unable to upload user data to store")
			return
		}
		w.publishStdout(color.GreenString(
			"✱ ✱ The build folder has been uploaded to " + key +
				". The data will be present for only a short duration of time.",
		))
	}()

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
		exec.Dir = buildDir

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
