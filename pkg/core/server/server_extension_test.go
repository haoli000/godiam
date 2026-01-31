// Copyright (c) 2026 Hao Li
// SPDX-License-Identifier: BSD-3-Clause

package server

import (
	"testing"

	"github.com/haoli000/godiam/pkg/core/config"
	"github.com/haoli000/godiam/pkg/core/extension"
	"github.com/haoli000/godiam/pkg/proto/dictionary"
)

type mockExtension struct {
	initialized bool
}

func (m *mockExtension) Name() string { return "mock_ext" }
func (m *mockExtension) Init(_ extension.InitContext, _ map[string]interface{}) error {
	m.initialized = true
	return nil
}

func TestServerExtensionInitialization(t *testing.T) {
	mock := &mockExtension{}
	extension.Register(mock)
	defer extension.Unregister("mock_ext")

	cfg := &config.Config{
		Identity: "server.test.com",
		Realm:    "test.com",
		Extensions: []config.ExtensionConfig{
			{Name: "mock_ext"},
		},
	}

	dict := dictionary.New()
	srv := New(cfg, dict)

	if err := srv.startExtensions(); err != nil {
		t.Fatalf("Failed to start extensions: %v", err)
	}

	if !mock.initialized {
		t.Error("Extension was not initialized")
	}
}
