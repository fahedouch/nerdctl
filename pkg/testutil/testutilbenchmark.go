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

package testutil

import (
	"os/exec"
	"testing"
)

/****************************/
/***** BenchMark Afero ******/
/****************************/

func NewBaseBenchmark(b *testing.B) *Base {
	base := &Base{
		T:                b,
		Target:           GetTarget(),
		DaemonIsKillable: GetDaemonIsKillable(),
	}
	var err error
	switch base.Target {
	case Nerdctl:
		base.Binary, err = exec.LookPath("nerdctl")
		if err != nil {
			b.Fatal(err)
		}
		base.Args = []string{"--namespace=" + Namespace}
		base.ComposeBinary = ""
	case Docker:
		base.Binary, err = exec.LookPath("docker")
		if err != nil {
			b.Fatal(err)
		}
		base.ComposeBinary, err = exec.LookPath("docker-compose")
		if err != nil {
			b.Fatal(err)
		}
	default:
		b.Fatalf("unknown test target %q", base.Target)
	}
	return base
}

/****************************/
/***** BenchMark Afero ******/
/****************************/
