package server

import (
	"sync"

	"github.com/Masterminds/semver"
	"github.com/pkg/errors"
	"github.com/rai-project/config"
)

var serverVersion *semver.Version
var serverVersionConstraint *semver.Constraints

func getVersionConstraint() (*semver.Constraints, error) {
	var once sync.Once
	once.Do(func() {
		constraint, err := semver.NewConstraint(">= " + config.App.Version.Version)
		if err != nil {
			return
		}
		serverVersionConstraint = constraint
	})
	if serverVersionConstraint == nil {
		return nil, errors.Errorf("the server version %v is not valid", config.App.Version)
	}
	return serverVersionConstraint, nil
}

func getVersion() (*semver.Version, error) {
	var once sync.Once
	once.Do(func() {
		ver, err := semver.NewVersion(config.App.Version.Version)
		if err != nil {
			return
		}
		serverVersion = ver
	})
	if serverVersion == nil {
		return nil, errors.Errorf("the server version %v is not valid", config.App.Version)
	}
	return serverVersion, nil
}
