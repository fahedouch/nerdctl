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

package main

import (
	"context"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
    "fmt"
	"github.com/containerd/containerd"
	"github.com/containerd/containerd/errdefs"
	"github.com/containerd/containerd/mount"
	"github.com/containerd/containerd/oci"
	"github.com/containerd/continuity/fs"
	"github.com/containerd/nerdctl/pkg/idgen"
	"github.com/containerd/nerdctl/pkg/imgutil"
	"github.com/containerd/nerdctl/pkg/mountutil"
	"github.com/containerd/nerdctl/pkg/strutil"
	"github.com/opencontainers/image-spec/identity"
	"github.com/opencontainers/runtime-spec/specs-go"
	"github.com/pkg/errors"
	"github.com/sirupsen/logrus"
	"github.com/urfave/cli/v2"
	"github.com/containerd/containerd/pkg/progress"
)

func generateMountOpts(clicontext *cli.Context, ctx context.Context, client *containerd.Client, ensuredImage *imgutil.EnsuredImage) ([]oci.SpecOpts, []string, error) {
	volStore, err := getVolumeStore(clicontext)
	if err != nil {
		return nil, nil, err
	}

	//nolint:golint,prealloc
	var (
		opts        []oci.SpecOpts
		anonVolumes []string
	)
	mounted := make(map[string]struct{})
	var imageVolumes map[string]struct{}
	var tempDir string

	if ensuredImage != nil {
		imageVolumes = ensuredImage.ImageConfig.Volumes

		if err := ensuredImage.Image.Unpack(ctx, clicontext.String("snapshotter")); err != nil {
			return nil, nil, errors.Wrap(err, "error unpacking image")
		}

		diffIDs, err := ensuredImage.Image.RootFS(ctx)
		if err != nil {
			return nil, nil, err
		}
		chainID := identity.ChainID(diffIDs).String()
        fmt.Println(chainID)
		s := client.SnapshotService(clicontext.String("snapshotter"))
        usage1, _ := s.Usage(ctx, chainID)
        totalSize := usage1.Size
        fmt.Println(progress.Bytes(totalSize))
		/*if err := s.Walk(ctx, func(ctx context.Context, info snapshots.Info) error {
			fmt.Println(
				info.Name,
				info.Parent,
				info.Kind)
			return nil
		}); err != nil {
			return nil, nil, err
		}*/
		tempDir, err = ioutil.TempDir("", "initialC")
		if err != nil {
			return nil, nil, err
		}
		defer os.RemoveAll(tempDir)

		var mounts []mount.Mount
		mounts, err = s.View(ctx, tempDir, chainID)
		if err != nil {
			return nil, nil, err
		}

		if err := mount.All(mounts, tempDir); err != nil {
			if err := s.Remove(ctx, tempDir); err != nil && !errdefs.IsNotFound(err) {
				return nil, nil, err
			}
			return nil, nil, err
		}
	}

	if flagVSlice := strutil.DedupeStrSlice(clicontext.StringSlice("v")); len(flagVSlice) > 0 {
		ociMounts := make([]specs.Mount, len(flagVSlice))
		for i, v := range flagVSlice {
			x, err := mountutil.ProcessFlagV(v, volStore)
			if err != nil {
				return nil, nil, err
			}
			ociMounts[i] = x.Mount
			mounted[filepath.Clean(x.Mount.Destination)] = struct{}{}

			//copying up initial contents of the mount point directory
			for imgVolRaw := range imageVolumes {
				imgVol := filepath.Clean(imgVolRaw)
				target := strings.Join([]string{tempDir, imgVol}, "")

				//Coyping content in AnonymousVolume and namedVolume
				if x.Mount.Destination == imgVol && x.Type == "volume" {
					if err := copyExistingContents(target, x.Mount.Source); err != nil {
						return nil, nil, err
					}
				}
			}
			if x.AnonymousVolume != "" {
				anonVolumes = append(anonVolumes, x.AnonymousVolume)
			}
		}
		opts = append(opts, oci.WithMounts(ociMounts))
	}

	// imageVolumes are defined in Dockerfile "VOLUME" instruction
	for imgVolRaw := range imageVolumes {
		imgVol := filepath.Clean(imgVolRaw)
		switch imgVol {
		case "/", "/dev", "/sys", "proc":
			return nil, nil, errors.Errorf("invalid VOLUME: %q", imgVolRaw)
		}
		if _, ok := mounted[imgVol]; ok {
			continue
		}
		anonVolName := idgen.GenerateID()

		logrus.Debugf("creating anonymous volume %q, for \"VOLUME %s\"",
			anonVolName, imgVolRaw)
		anonVol, err := volStore.Create(anonVolName)
		if err != nil {
			return nil, nil, err
		}

		target := strings.Join([]string{tempDir, imgVol}, "")
		//copying up initial contents of the mount point directory
		if err := copyExistingContents(target, anonVol.Mountpoint); err != nil {
			return nil, nil, err
		}

		m := []specs.Mount{
			{
				Type:        "none",
				Source:      anonVol.Mountpoint,
				Destination: imgVol,
				Options:     []string{"rbind"},
			},
		}

		opts = append(opts, oci.WithMounts(m))
		anonVolumes = append(anonVolumes, anonVolName)
	}

	return opts, anonVolumes, nil
}

// copyExistingContents copies from the source to the destination and
// ensures the ownership is appropriately set.
func copyExistingContents(source, destination string) error {
	if _, err := os.Stat(source); os.IsNotExist(err) {
		return nil
	}
	dstList, err := ioutil.ReadDir(destination)
	if err != nil {
		return err
	}
	if len(dstList) != 0 {
		return errors.Errorf("volume at %q is not initially empty", destination)
	}
	return fs.CopyDir(destination, source)
}
