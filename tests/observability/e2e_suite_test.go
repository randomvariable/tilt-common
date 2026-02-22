// Copyright 2026 Naadir Jeewa
// SPDX-License-Identifier: Apache-2.0

//go:build e2e

package observability_test

import (
	"testing"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
)

func TestE2E(t *testing.T) {
	RegisterFailHandler(Fail)
	RunSpecs(t, "Observability Stack E2E Suite")
}
