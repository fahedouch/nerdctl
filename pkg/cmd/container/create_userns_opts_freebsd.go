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

package container

import (
	"context"

	containerd "github.com/containerd/containerd/v2/client"
	"github.com/containerd/containerd/v2/pkg/oci"

	"github.com/containerd/nerdctl/v2/pkg/api/types"
	"github.com/containerd/nerdctl/v2/pkg/containerutil"
	"github.com/containerd/nerdctl/v2/pkg/imgutil"
)

func getUserNamespaceOpts(
	ctx context.Context,
	client *containerd.Client,
	options *types.ContainerCreateOptions,
	ensuredImage imgutil.EnsuredImage,
	id string,
) ([]oci.SpecOpts, []containerd.NewContainerOpts, error) {
	return []oci.SpecOpts{}, []containerd.NewContainerOpts{}, nil
}

func getContainerUserNamespaceNetOpts(
	ctx context.Context,
	client *containerd.Client,
	netManager containerutil.NetworkOptionsManager,
) ([]oci.SpecOpts, error) {
	return []oci.SpecOpts{}, nil
}
