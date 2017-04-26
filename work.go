package server

import (
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"time"

	"bytes"

	"github.com/Unknwon/com"
	azaws "github.com/aws/aws-sdk-go/aws"
	"github.com/fatih/color"
	"github.com/pkg/errors"
	"github.com/rai-project/archive"
	"github.com/rai-project/aws"
	"github.com/rai-project/config"
	"github.com/rai-project/docker"
	"github.com/rai-project/model"
	nvidiasmi "github.com/rai-project/nvidia-smi"
	"github.com/rai-project/pubsub"
	"github.com/rai-project/pubsub/redis"
	"github.com/rai-project/store"
	"github.com/rai-project/store/s3"
	"github.com/rai-project/uuid"
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
		docker.Stdin(nil),
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
		ID:        uuid.NewV4(),
		Kind:      model.StdoutResponse,
		Body:      []byte(s),
		CreatedAt: time.Now(),
	})
}

func (w *WorkRequest) publishStderr(s string) error {
	return w.publisher.Publish(w.publishChannel, model.JobResponse{
		ID:        uuid.NewV4(),
		Kind:      model.StderrResponse,
		Body:      []byte(s),
		CreatedAt: time.Now(),
	})
}

func (w *WorkRequest) buildImage(spec *model.BuildImageSpecification, uploadedReader io.Reader) error {
	if spec == nil {
		return nil
	}

	if spec.ImageName == "" {
		spec.ImageName = uuid.NewV4()
	}

	appName := strings.TrimSuffix(config.App.Name, "d")
	if strings.HasPrefix(spec.ImageName, appName) {
		w.publishStderr(color.RedString("✱ Docker image name cannot start with " + appName + "/ . Choose a different name."))
		return errors.New("docker image namespace")
	}

	if w.docker.HasImage(spec.ImageName) && spec.NoCache == false {
		w.publishStdout(color.YellowString("✱ Using cached version of the docker image. Set no_cache=true to disable cache."))
		return nil
	}

	tmpDir, err := ioutil.TempDir(config.App.TempDir, "buildImage")
	if err != nil {
		w.publishStderr(color.RedString("✱ Server was unable to create a temporary directory."))
		return err
	}
	defer os.RemoveAll(tmpDir)

	if err := archive.Unzip(uploadedReader, tmpDir); err != nil {
		w.publishStderr(color.RedString("✱ Unable to unzip your folder " + err.Error() + "."))
		return err
	}

	if spec.Dockerfile == "" {
		spec.Dockerfile = "Dockerfile"
	}

	dockerfile := filepath.Join(tmpDir, spec.Dockerfile)
	if !com.IsFile(dockerfile) {
		w.publishStderr(color.RedString("✱ Unable to find Dockerfile. Make sure the path is specified correctly."))
		return errors.Errorf("file %v not found", dockerfile)
	}

	f, err := os.Open(dockerfile)
	if err != nil {
		w.publishStderr(color.RedString("✱ Unable to open Dockerfile."))
		return err
	}
	defer f.Close()

	w.publishStdout(color.YellowString("✱ Server is starting to build image."))
	if err := w.docker.ImageBuild(spec.ImageName, f); err != nil {
		w.publishStderr(color.RedString("✱ Unable to build dockerfile."))
		return err
	}

	w.publishStdout(color.YellowString("✱ Server has build your docker image."))
	return nil
}

func (w *WorkRequest) Start() error {
	buildSpec := w.buildSpec

	defer func() {
		if r := recover(); r != nil {
			w.publishStderr(color.RedString("✱ Server crashed while processing your request."))
		}
	}()

	defer func() {
		w.publishStdout(color.GreenString("✱ Server has ended your request."))
		w.publisher.End(w.publishChannel)
	}()

	w.publishStdout(color.YellowString("✱ Server has accepted your job submission and started to configure the container."))

	w.publishStdout(color.YellowString("✱ Downloading your code."))

	buf := new(azaws.WriteAtBuffer)
	if err := w.store.DownloadTo(buf, w.UploadKey); err != nil {
		w.publishStderr(color.RedString("✱ Failed to download your code."))
		log.WithError(err).WithField("key", w.UploadKey).Error("failed to download user code from store")
		return err
	}

	err := w.buildImage(buildSpec.Commands.BuildImage, bytes.NewBuffer(buf.Bytes()))
	if err != nil {
		w.publishStderr(color.RedString("✱ Unable to create image " + buildSpec.Commands.BuildImage.ImageName + "."))
		return err
	} else if buildSpec.Commands.BuildImage != nil {
		buildSpec.RAI.ContainerImage = buildSpec.Commands.BuildImage.ImageName
	}

	imageName := buildSpec.RAI.ContainerImage
	w.publishStdout(color.YellowString("✱ Using " + imageName + " as container image."))

	if buildSpec.Commands.BuildImage == nil && !w.docker.HasImage(imageName) {
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
		docker.Image(imageName),
		docker.AddEnv("IMAGE_NAME", imageName),
		docker.AddVolume(srcDir),
		docker.AddVolume(buildDir),
		docker.WorkingDirectory(buildDir),
		docker.Shell([]string{"/bin/bash"}),
		docker.Entrypoint([]string{}),
	}
	if buildSpec.Resources.Network {
		containerOpts = append(containerOpts, docker.NetworkDisabled(!buildSpec.Resources.Network))
	}
	if buildSpec.Resources.GPU != nil {
		if buildSpec.Resources.GPU.Count > len(nvidiasmi.Info.GPUS) {
			w.publishStderr(color.RedString(
				fmt.Sprintf("✱ The maximum number of gpus available on the machine is %d. You're launch will utilitize those gpus.",
					len(nvidiasmi.Info.GPUS))))
			buildSpec.Resources.GPU.Count = len(nvidiasmi.Info.GPUS)
		}
		for ii := 0; ii < buildSpec.Resources.GPU.Count; ii++ {
			containerOpts = append(containerOpts, docker.CUDADevice(ii))
		}
		containerOpts = append(containerOpts, docker.NvidiaVolume(""))
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

		uploadKey := opts.clientUploadDestinationDirectory + "/build-" + w.ID + "." + archive.Extension()
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
			"✱ The build folder has been uploaded to " + key +
				". The data will be present for only a short duration of time.",
		))
	}()

	for _, cmd := range buildSpec.Commands.Build {
		cmd = strings.TrimSpace(cmd)
		if cmd == "" {
			continue
		}
		/*
			if !strings.HasPrefix(cmd, "/bin/sh") && !strings.HasPrefix(cmd, "/bin/sh") {
				cmd = "/bin/sh -c " + cmd
			}
		*/
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
	//log.WithField("id", w.ID).WithField("image", imageName).Debug("finished ")

	return nil
}

func (w *WorkRequest) Close() error {
	if w.container != nil {
		w.container.Close()
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
