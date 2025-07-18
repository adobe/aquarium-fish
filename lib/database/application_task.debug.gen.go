//go:build debug

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

// Code generated by trace-gen-functions tool. DO NOT EDIT.

package database

import (
	"context"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"

	typesv2 "github.com/adobe/aquarium-fish/lib/types/aquarium/v2"
)

var debugTracerDatabaseApplication_task = otel.Tracer("aquarium-fish/database")

func (d *Database) SubscribeApplicationTask(ctx context.Context, ch chan *typesv2.ApplicationTask) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.subscribeApplicationTaskImpl")
	defer span.End()

	d.subscribeApplicationTaskImpl(ctx, ch)

}

func (d *Database) UnsubscribeApplicationTask(ctx context.Context, ch chan *typesv2.ApplicationTask) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.unsubscribeApplicationTaskImpl")
	defer span.End()

	d.unsubscribeApplicationTaskImpl(ctx, ch)

}

func (d *Database) ApplicationTaskList(ctx context.Context) ([]typesv2.ApplicationTask, error) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskListImpl")
	defer span.End()

	at, err1 := d.applicationTaskListImpl(ctx)

	if err1 != nil {
		span.RecordError(err1)
		span.SetStatus(codes.Error, err1.Error())
	}

	return at, err1

}

func (d *Database) ApplicationTaskListByApplication(ctx context.Context, appUID typesv2.ApplicationUID) ([]typesv2.ApplicationTask, error) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskListByApplicationImpl")
	defer span.End()

	at, err1 := d.applicationTaskListByApplicationImpl(ctx, appUID)

	if err1 != nil {
		span.RecordError(err1)
		span.SetStatus(codes.Error, err1.Error())
	}

	return at, err1

}

func (d *Database) ApplicationTaskCreate(ctx context.Context, at *typesv2.ApplicationTask) error {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskCreateImpl")
	defer span.End()

	err0 := d.applicationTaskCreateImpl(ctx, at)

	if err0 != nil {
		span.RecordError(err0)
		span.SetStatus(codes.Error, err0.Error())
	}

	return err0

}

func (d *Database) ApplicationTaskSave(ctx context.Context, at *typesv2.ApplicationTask) error {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskSaveImpl")
	defer span.End()

	err0 := d.applicationTaskSaveImpl(ctx, at)

	if err0 != nil {
		span.RecordError(err0)
		span.SetStatus(codes.Error, err0.Error())
	}

	return err0

}

func (d *Database) ApplicationTaskGet(ctx context.Context, uid typesv2.ApplicationTaskUID) (*typesv2.ApplicationTask, error) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskGetImpl")
	defer span.End()

	at, err1 := d.applicationTaskGetImpl(ctx, uid)

	if err1 != nil {
		span.RecordError(err1)
		span.SetStatus(codes.Error, err1.Error())
	}

	return at, err1

}

func (d *Database) ApplicationTaskDelete(ctx context.Context, uid typesv2.ApplicationTaskUID) error {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskDeleteImpl")
	defer span.End()

	err0 := d.applicationTaskDeleteImpl(ctx, uid)

	if err0 != nil {
		span.RecordError(err0)
		span.SetStatus(codes.Error, err0.Error())
	}

	return err0

}

func (d *Database) ApplicationTaskListByApplicationAndWhen(ctx context.Context, appUID typesv2.ApplicationUID, when typesv2.ApplicationState_Status) ([]typesv2.ApplicationTask, error) {
	ctx, span := debugTracerDatabaseApplication_task.Start(ctx, "database.Database.applicationTaskListByApplicationAndWhenImpl")
	defer span.End()

	at, err1 := d.applicationTaskListByApplicationAndWhenImpl(ctx, appUID, when)

	if err1 != nil {
		span.RecordError(err1)
		span.SetStatus(codes.Error, err1.Error())
	}

	return at, err1

}
