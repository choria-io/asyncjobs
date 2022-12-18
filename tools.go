// Copyright (c) 2022, R.I. Pienaar and the Choria Project contributors
//
// SPDX-License-Identifier: Apache-2.0

//go:build tools

package main

import (
	_ "github.com/onsi/ginkgo/v2/ginkgo/generators"
	_ "github.com/onsi/ginkgo/v2/ginkgo/internal"
	_ "github.com/onsi/ginkgo/v2/ginkgo/labels"
)

// this file is here to make things like go generate and ginkgo install
// happy, it has dependencies imported that it does not use and the build
// constraint ensures it's excluded during normal builds.
//
// see https://github.com/golang/go/wiki/Modules#how-can-i-track-tool-dependencies-for-a-module
