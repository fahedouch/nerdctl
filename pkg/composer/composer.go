/*
   Copyright The containerd Authors.

   Licensed under the Apache License, Version 2.0 (the "License");
   you may not use this file except in compliance with the License.
   You may obtain a copy of the License at

       http://www.apache.org/licenses/LICENSE-2.0

   Unless required by applicable law or agreed to in writing, software
   distributed under the License is distributed on an "AS IS" BASIS,
   WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
   See the License for the specific language governing permissions and
   limitations under the License.
*/

package composer

import (
	"context"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"

	compose "github.com/compose-spec/compose-go/types"
	"github.com/containerd/containerd/identifiers"
	"github.com/containerd/nerdctl/pkg/composer/projectloader"
	"github.com/containerd/nerdctl/pkg/reflectutil"
	"github.com/joho/godotenv"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
)

type Options struct {
	//TODO Specifying multiple Compose files
	File           string // empty for default
	Project        string // empty for default
	EnvFile        string
	Environment    map[string]string
	WorkingDir     string
	NerdctlCmd     string
	NerdctlArgs    []string
	NetworkExists  func(string) (bool, error)
	VolumeExists   func(string) (bool, error)
	ImageExists    func(ctx context.Context, imageName string) (bool, error)
	EnsureImage    func(ctx context.Context, imageName, pullMode string) error
	DebugPrintFull bool // full debug print, may leak secret env var to logs
}

func New(o Options) (*Composer, error) {
	if o.NerdctlCmd == "" {
		return nil, errors.New("got empty nerdctl cmd")
	}
	if o.NetworkExists == nil || o.VolumeExists == nil || o.EnsureImage == nil {
		return nil, errors.New("got empty functions")
	}

	var err error
	if o.File == "" {
		o.File, err = findComposeYAML(&o)
		if err != nil {
			return nil, err
		}
	}

	if err := setDotEnv(&o); err != nil {
		return nil, err
	}

	o.File, err = filepath.Abs(o.File)
	if err != nil {
		return nil, err
	}

	if o.Project == "" {
		o.Project = filepath.Base(filepath.Dir(o.File))
	}

	if err := identifiers.Validate(o.Project); err != nil {
		return nil, errors.Wrapf(err, "got invalid project name %q", o.Project)
	}

	project, err := projectloader.Load(o.File, o.Project, o.Environment)
	if err != nil {
		return nil, err
	}

	if o.DebugPrintFull {
		projectJSON, _ := json.MarshalIndent(project, "", "    ")
		logrus.Debug("printing project JSON")
		logrus.Debugf("%s", projectJSON)
	}

	if unknown := reflectutil.UnknownNonEmptyFields(project,
		"Name",
		"WorkingDir",
		"Environment",
		"Services",
		"Networks",
		"Volumes",
		"Secrets",
		"Configs",
		"ComposeFiles"); len(unknown) > 0 {
		logrus.Warnf("Ignoring: %+v", unknown)
	}

	c := &Composer{
		Options: o,
		project: project,
	}

	return c, nil
}

type Composer struct {
	Options
	project *compose.Project
}

func (c *Composer) createNerdctlCmd(ctx context.Context, args ...string) *exec.Cmd {
	return exec.CommandContext(ctx, c.NerdctlCmd, append(c.NerdctlArgs, args...)...)
}

func (c *Composer) runNerdctlCmd(ctx context.Context, args ...string) error {
	cmd := c.createNerdctlCmd(ctx, args...)
	if c.DebugPrintFull {
		logrus.Debugf("Running %v", cmd.Args)
	}
	if out, err := cmd.CombinedOutput(); err != nil {
		return errors.Wrapf(err, "error while executing %v: %q", cmd.Args, string(out))
	}
	return nil
}

//find compose Yaml file in current directory and its parents
func findComposeYAML(o *Options) (string, error) {
	pwd, err := o.GetWorkingDir()
	if err != nil {
		return "", err
	}
	for {
		yamlNames := []string{"docker-compose.yml", "docker-compose.yaml", "compose.yml", "compose.yaml"}
		for _, candidate := range yamlNames {
			f := filepath.Join(pwd, candidate)
			if _, err := os.Stat(f); err == nil {
				return f, nil
			} else if !os.IsNotExist(err) {
				return "", err
			}
		}
		parent := filepath.Dir(pwd)
		if parent == pwd {
			return "", errors.Errorf("cannot find a compose YAML, supported file names: %+v in this directory or any parent", yamlNames)
		}
		pwd = parent
	}
}

// setDotEnv was inspired from https://github.com/compose-spec/compose-go/blob/de56f4f0cb3c925f41df594221148613534c2cd3/cli/options.go#L174-215
// setDotEnv imports environment variables from .env file
func setDotEnv(o *Options) error {
	dotEnvFile := o.EnvFile
	if dotEnvFile == "" {
		wd, err := o.GetWorkingDir()
		if err != nil {
			return err
		}
		dotEnvFile = filepath.Join(wd, ".env")
	}
	abs, err := filepath.Abs(dotEnvFile)
	if err != nil {
		return err
	}
	dotEnvFile = abs
	s, err := os.Stat(dotEnvFile)
	if os.IsNotExist(err) {
		if o.EnvFile != "" {
			return err
		}
		return nil
	}
	if err != nil {
		return err
	}

	if s.IsDir() {
		return errors.Errorf("%s is not a file", dotEnvFile)
	}

	file, err := os.Open(dotEnvFile)
	if err != nil {
		return err
	}
	defer file.Close()

	env, err := godotenv.Parse(file)
	if err != nil {
		return err
	}
	for k, v := range env {
		o.Environment[k] = v
	}
	return nil
}

func (o Options) GetWorkingDir() (string, error) {
	if o.WorkingDir != "" {
		return o.WorkingDir, nil
	}
	// reading the configuration from stdin
	if o.File != "-" {
		absPath, err := filepath.Abs(o.File)
		if err != nil {
			return "", err
		}
		return filepath.Dir(absPath), nil
	}
	return os.Getwd()
}
