// Copyright 2021 Yoshi Yamaguchi
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
// See the License for the specific language governing permissions and
// limitations under the License.

package main

import (
	"github.com/NVIDIA/gpu-monitoring-tools/bindings/go/nvml"
)

func init() {
	if err := nvml.Init(); err != nil {
		logger.Error().Msgf("Failed to initialize NVML: %v", err)
	}
}

func main() {
	defer func() {
		if err := nvml.Shutdown(); err != nil {
			logger.Error().Msgf("Failed to shutdown NVML: %v", err)
		}
	}()
}
