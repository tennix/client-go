// Copyright 2017 PingCAP, Inc.
//
// Licensed under the Apache License, Version 2.0 (the "License");
// you may not use this file except in compliance with the License.
// You may obtain a copy of the License at
//
//     http://www.apache.org/licenses/LICENSE-2.0
//
// Unless required by applicable law or agreed to in writing, software
// distributed under the License is distributed on an "AS IS" BASIS,
// See the License for the specific language governing permissions and
// limitations under the License.

package store

import (
	"context"
	"crypto/tls"
	"strconv"
	"sync"
	"time"

	"go.etcd.io/etcd/clientv3"
	grpc_prometheus "github.com/grpc-ecosystem/go-grpc-prometheus"
	"github.com/pkg/errors"
	log "github.com/sirupsen/logrus"
	"google.golang.org/grpc"
)

// SafePointKV is used for a seamingless integration for mockTest and runtime.
type SafePointKV interface {
	Put(k string, v string) error
	Get(k string) (string, error)
}

// MockSafePointKV implements SafePointKV at mock test
type MockSafePointKV struct {
	store    map[string]string
	mockLock sync.RWMutex
}

// NewMockSafePointKV creates an instance of MockSafePointKV
func NewMockSafePointKV() *MockSafePointKV {
	return &MockSafePointKV{
		store: make(map[string]string),
	}
}

// Put implements the Put method for SafePointKV
func (w *MockSafePointKV) Put(k string, v string) error {
	w.mockLock.Lock()
	defer w.mockLock.Unlock()
	w.store[k] = v
	return nil
}

// Get implements the Get method for SafePointKV
func (w *MockSafePointKV) Get(k string) (string, error) {
	w.mockLock.RLock()
	defer w.mockLock.RUnlock()
	elem := w.store[k]
	return elem, nil
}

// EtcdSafePointKV implements SafePointKV at runtime
type EtcdSafePointKV struct {
	cli *clientv3.Client
}

// NewEtcdSafePointKV creates an instance of EtcdSafePointKV
func NewEtcdSafePointKV(addrs []string, tlsConfig *tls.Config) (*EtcdSafePointKV, error) {
	etcdCli, err := createEtcdKV(addrs, tlsConfig)
	if err != nil {
		return nil, err
	}
	return &EtcdSafePointKV{cli: etcdCli}, nil
}

func createEtcdKV(addrs []string, tlsConfig *tls.Config) (*clientv3.Client, error) {
	cli, err := clientv3.New(clientv3.Config{
		Endpoints:   addrs,
		DialTimeout: 5 * time.Second,
		DialOptions: []grpc.DialOption{
			grpc.WithUnaryInterceptor(grpc_prometheus.UnaryClientInterceptor),
			grpc.WithStreamInterceptor(grpc_prometheus.StreamClientInterceptor),
		},
		TLS: tlsConfig,
	})
	if err != nil {
		return nil, errors.WithStack(err)
	}
	return cli, nil
}

// Put implements the Put method for SafePointKV
func (w *EtcdSafePointKV) Put(k string, v string) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	_, err := w.cli.Put(ctx, k, v)
	cancel()
	if err != nil {
		return errors.WithStack(err)
	}
	return nil
}

// Get implements the Get method for SafePointKV
func (w *EtcdSafePointKV) Get(k string) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*5)
	resp, err := w.cli.Get(ctx, k)
	cancel()
	if err != nil {
		return "", errors.WithStack(err)
	}
	if len(resp.Kvs) > 0 {
		return string(resp.Kvs[0].Value), nil
	}
	return "", nil
}

func saveSafePoint(kv SafePointKV, key string, t uint64) error {
	s := strconv.FormatUint(t, 10)
	err := kv.Put(key, s)
	if err != nil {
		log.Error("save safepoint failed:", err)
		return err
	}
	return nil
}

func loadSafePoint(kv SafePointKV, key string) (uint64, error) {
	str, err := kv.Get(key)

	if err != nil {
		return 0, err
	}

	if str == "" {
		return 0, nil
	}

	t, err := strconv.ParseUint(str, 10, 64)
	if err != nil {
		return 0, errors.WithStack(err)
	}
	return t, nil
}
