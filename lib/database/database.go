/**
 * Copyright 2025 Adobe. All rights reserved.
 * This file is licensed to you under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License. You may obtain a copy
 * of the License at http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software distributed under
 * the License is distributed on an "AS IS" BASIS, WITHOUT WARRANTIES OR REPRESENTATIONS
 * OF ANY KIND, either express or implied. See the License for the specific language
 * governing permissions and limitations under the License.
 */

// Author: Sergei Parshev (@sparshev)

// Database management for the Fish node
package database

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/google/uuid"
	"go.mills.io/bitcask/v2"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/adobe/aquarium-fish/lib/log"
	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

var ErrObjectNotFound = bitcask.ErrObjectNotFound

// Database implements necessary functions to manipulate the internal database
type Database struct {
	// Backend used to store the data
	be *bitcask.Bitcask

	// TODO: This mutex is needed until the issue with Merge will be resolved: https://git.mills.io/prologic/bitcask/issues/276
	// I use this mutex to write-lock the database when merge is happening, the other operations just
	// uses RLock to not interfere with the merge operation.
	beMu sync.RWMutex

	// Memory storage for current node - we using it to generate new UIDs
	node typesv2.Node

	// Subscriptions to notify subscribers about changes in DB, contains key prefix and channel
	// Protected by subsMu to prevent data races during subscribe/unsubscribe operations
	subsMu                  sync.RWMutex
	subsApplicationState    []chan *typesv2.ApplicationState
	subsApplicationTask     []chan *typesv2.ApplicationTask
	subsApplicationResource []chan *typesv2.ApplicationResource

	// OpenTelemetry instrumentation
	tracer trace.Tracer
	meter  metric.Meter

	// Metrics
	dbOperationDuration metric.Float64Histogram
	dbOperationCounter  metric.Int64Counter
	dbSizeGauge         metric.Int64Gauge
	dbKeysGauge         metric.Int64Gauge
	dbReclaimableGauge  metric.Int64Gauge
}

// Init creates the database object by provided path
func New(path string) (*Database, error) {
	if err := os.MkdirAll(path, 0o750); err != nil {
		log.Error().Msgf("DB: Can't create working directory %s: %v", path, err)
		return nil, fmt.Errorf("DB: Can't create working directory %s: %v", path, err)
	}

	be, err := bitcask.Open(filepath.Join(path, "bitcask.db"))
	if err != nil {
		log.Error().Msgf("DB: Unable to initialize database: %v", err)
		return nil, fmt.Errorf("DB: Unable to initialize database: %v", err)
	}

	db := &Database{be: be}

	// Initialize OpenTelemetry instrumentation
	db.tracer = otel.Tracer("aquarium-fish-database")
	db.meter = otel.Meter("aquarium-fish-database")

	// Create metrics
	db.dbOperationDuration, _ = db.meter.Float64Histogram(
		"aquarium_fish_db_operation_duration_seconds",
		metric.WithDescription("Duration of database operations"),
		metric.WithUnit("s"),
	)

	db.dbOperationCounter, _ = db.meter.Int64Counter(
		"aquarium_fish_db_operations_total",
		metric.WithDescription("Total number of database operations"),
	)

	db.dbSizeGauge, _ = db.meter.Int64Gauge(
		"aquarium_fish_db_size_bytes",
		metric.WithDescription("Current database size in bytes"),
	)

	db.dbKeysGauge, _ = db.meter.Int64Gauge(
		"aquarium_fish_db_keys_total",
		metric.WithDescription("Total number of keys in database"),
	)

	db.dbReclaimableGauge, _ = db.meter.Int64Gauge(
		"aquarium_fish_db_reclaimable_bytes",
		metric.WithDescription("Reclaimable space in database"),
	)

	return db, nil
}

// CompactDB runs stale Applications and data removing
func (d *Database) CompactDB() error {
	return d.CompactDBWithContext(context.Background())
}

// CompactDBWithContext runs stale Applications and data removing with context
func (d *Database) CompactDBWithContext(ctx context.Context) error {
	ctx, span := d.tracer.Start(ctx, "database.compact")
	defer span.End()

	start := time.Now()
	defer func() {
		duration := time.Since(start).Seconds()
		d.dbOperationDuration.Record(ctx, duration, metric.WithAttributes(
			attribute.String("operation", "compact"),
		))
		d.dbOperationCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "compact"),
		))
	}()

	log.Debug().Msg("DB: CompactDB locking...")
	defer log.Debug().Msg("Fish: CompactDB done")

	// Locking entire database
	d.beMu.Lock()
	defer d.beMu.Unlock()
	log.Debug().Msg("DB: CompactDB running...")

	s, _ := d.be.Stats()
	log.Debug().Msgf("DB: CompactDB: Before compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)

	// Record metrics before compaction
	d.dbSizeGauge.Record(ctx, int64(s.Size))
	d.dbKeysGauge.Record(ctx, int64(s.Keys))
	d.dbReclaimableGauge.Record(ctx, int64(s.Reclaimable))

	span.SetAttributes(
		attribute.Int64("db.size_before", int64(s.Size)),
		attribute.Int64("db.keys_before", int64(s.Keys)),
		attribute.Int64("db.reclaimable_before", int64(s.Reclaimable)),
	)

	if err := d.be.Merge(); err != nil {
		span.RecordError(err)
		d.dbOperationCounter.Add(ctx, 1, metric.WithAttributes(
			attribute.String("operation", "compact"),
			attribute.String("result", "error"),
		))
		log.Error().Msgf("DB: CompactDB: Merge operation failed: %v", err)
		return fmt.Errorf("DB: CompactDB: Merge operation failed: %v", err)
	}

	s, _ = d.be.Stats()
	log.Debug().Msgf("DB: CompactDB: After compaction: Datafiles: %d, Keys: %d, Size: %d, Reclaimable: %d", s.Datafiles, s.Keys, s.Size, s.Reclaimable)

	// Record metrics after compaction
	d.dbSizeGauge.Record(ctx, int64(s.Size))
	d.dbKeysGauge.Record(ctx, int64(s.Keys))
	d.dbReclaimableGauge.Record(ctx, int64(s.Reclaimable))

	span.SetAttributes(
		attribute.Int64("db.size_after", int64(s.Size)),
		attribute.Int64("db.keys_after", int64(s.Keys)),
		attribute.Int64("db.reclaimable_after", int64(s.Reclaimable)),
	)

	d.dbOperationCounter.Add(ctx, 1, metric.WithAttributes(
		attribute.String("operation", "compact"),
		attribute.String("result", "success"),
	))

	return nil
}

// Shutdown compacts database backend and closes it
func (d *Database) Shutdown() error {
	d.CompactDB()

	// Waiting for all the current requests to be done by acquiring write lock and closing the DB
	d.beMu.Lock()
	defer d.beMu.Unlock()

	if err := d.be.Close(); err != nil {
		log.Error().Msgf("DB: Unable to close backend: %v", err)
		return fmt.Errorf("DB: Unable to close backend: %v", err)
	}

	return nil
}

// SetNode puts current node in the memory storage
func (d *Database) SetNode(node typesv2.Node) {
	d.node = node
}

// GetNode returns current Fish node spec
func (d *Database) GetNode() *typesv2.Node {
	return &d.node
}

// GetNodeUID returns node UID
func (d *Database) GetNodeUID() typesv2.NodeUID {
	return d.node.Uid
}

// GetNodeName returns current node name
func (d *Database) GetNodeName() string {
	return d.node.Name
}

// GetNodeLocation returns current node location
func (d *Database) GetNodeLocation() string {
	return d.node.Location
}

// NewUID Creates new UID with 6 starting bytes of Node UID as prefix
func (d *Database) NewUID() uuid.UUID {
	uid := uuid.New()
	copy(uid[:], d.node.Uid[:6])
	return uid
}
