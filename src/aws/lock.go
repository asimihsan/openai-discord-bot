/*
 * Copyright (C) 2023 Asim Ihsan
 * SPDX-License-Identifier: AGPL-3.0-only
 *
 * This program is free software: you can redistribute it and/or modify it under
 * the terms of the GNU Affero General Public License as published by the Free
 * Software Foundation, version 3.
 *
 * This program is distributed in the hope that it will be useful, but WITHOUT ANY
 * WARRANTY; without even the implied warranty of MERCHANTABILITY or FITNESS FOR A
 * PARTICULAR PURPOSE. See the GNU Affero General Public License for more details.
 *
 * You should have received a copy of the GNU Affero General Public License along
 * with this program. If not, see <https://www.gnu.org/licenses/>
 */

package aws

import (
	"context"
)

type Lock struct {
	ID                          string
	Owner                       string
	LeaseDurationMilliseconds   int64
	LastUpdatedTimeMilliseconds int64
	RecordVersionNumber         string
	Shard                       int64
	TTLEpochSeconds             int64
	CreatedAtMilliseconds       int64
	Data                        interface{}
}

func (l *Lock) IsExpired(nowMilliseconds int64) bool {
	return nowMilliseconds-l.LastUpdatedTimeMilliseconds > l.LeaseDurationMilliseconds
}

type LockClient interface {
	Acquire(ctx context.Context, id string, data interface{}) (*Lock, error)
	Heartbeat(ctx context.Context, id string, maybeNewData *interface{}) error
	Release(ctx context.Context, id string) error
	Close() error
	Owner() string
}

func NewLock(
	ID string,
	Owner string,
	LeaseDurationMilliseconds int64,
	LastUpdatedTimeMilliseconds int64,
	RecordVersionNumber string,
	Shard int64,
	TTLEpochSeconds int64,
	CreatedAtMilliseconds int64,
	Data interface{},
) Lock {
	return Lock{
		ID:                          ID,
		Owner:                       Owner,
		LeaseDurationMilliseconds:   LeaseDurationMilliseconds,
		LastUpdatedTimeMilliseconds: LastUpdatedTimeMilliseconds,
		RecordVersionNumber:         RecordVersionNumber,
		Shard:                       Shard,
		TTLEpochSeconds:             TTLEpochSeconds,
		CreatedAtMilliseconds:       CreatedAtMilliseconds,
		Data:                        Data,
	}
}

func PtrToLock(l Lock) *Lock {
	return &l
}
