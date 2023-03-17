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
	"encoding/json"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/feature/dynamodb/expression"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodbtypes "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gofrs/uuid"
	"github.com/hashicorp/go-multierror"
	"github.com/rs/zerolog"
	"math/rand"
	"strconv"
	"sync"
	"time"
)

var (
	LockNotFoundError                = errors.New("lock not found")
	LockHeartbeatFailedError         = errors.New("failed to heartbeat lock")
	LockReleaseFailedError           = errors.New("failed to release lock")
	LockConditionalUpdateFailedError = errors.New("failed to update lock due to condition not being met")
	LockAbandonedError               = errors.New("lock abandoned")
)

type LockCurrentlyUnavailableError struct {
}

func (e LockCurrentlyUnavailableError) Error() string {
	return "lock is currently unavailable"
}

type DynamoDBLockConfig struct {
	Owner                    string
	MaxShards                int
	LeaseDurationSeconds     int
	HeartbeatIntervalSeconds int
}

type DynamoDBLockClient struct {
	Client             *dynamodb.Client
	TableName          string
	Config             DynamoDBLockConfig
	locks              map[string]Lock
	mu                 sync.Mutex
	stopBackgroundJobs chan struct{}
	zlog               *zerolog.Logger
}

func NewDynamoDBClient(region string) (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
		config.WithRetryMaxAttempts(3),
		config.WithDefaultsMode(aws.DefaultsModeAuto),
	)
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(cfg), nil
}

func NewDynamoDBLockClient(
	tableName string,
	region string,
	config DynamoDBLockConfig,
	zlog *zerolog.Logger,
) (*DynamoDBLockClient, error) {
	client, err := NewDynamoDBClient(region)
	if err != nil {
		return nil, err
	}

	d := DynamoDBLockClient{
		Client:             client,
		TableName:          tableName,
		Config:             config,
		locks:              make(map[string]Lock),
		mu:                 sync.Mutex{},
		stopBackgroundJobs: make(chan struct{}),
		zlog:               zlog,
	}

	// Start a background job that once a minute heartbeat all locks that we own. There is another
	// channel that tells the background job to stop.
	go func() {
		ticker := time.NewTicker(time.Duration(d.Config.HeartbeatIntervalSeconds) * time.Second)
		for {
			select {
			case <-ticker.C:
				// Make a []string of lock IDs to heartbeat
				d.mu.Lock()
				lockIDs := make([]string, 0, len(d.locks))
				for lockID := range d.locks {
					lockIDs = append(lockIDs, lockID)
				}
				d.mu.Unlock()

				var wg sync.WaitGroup
				var errs multierror.Error
				for _, lockID := range lockIDs {
					wg.Add(1)
					go func(lockID string) {
						defer wg.Done()
						err := d.Heartbeat(context.TODO(), lockID, nil)
						if err != nil {
							// if we are abandoning a lock, remove it from the map
							if errors.Is(err, LockAbandonedError) {
								d.mu.Lock()
								delete(d.locks, lockID)
								d.mu.Unlock()
							}
							errs.Errors = append(errs.Errors, err)
						}
					}(lockID)
				}
				wg.Wait()
				if len(errs.Errors) > 0 {
					zlog.Error().Err(errs.ErrorOrNil()).Msg("failed to heartbeat locks")
				}

			case <-d.stopBackgroundJobs:
				zlog.Info().Msg("stopping background heartbeat job")
				return
			}
		}
	}()

	return &d, nil
}

func (d *DynamoDBLockClient) Close() error {
	d.stopBackgroundJobs <- struct{}{}
	return nil
}

func (d *DynamoDBLockClient) Owner() string {
	return d.Config.Owner
}

func (d *DynamoDBLockClient) Acquire(
	ctx context.Context,
	id string,
	data interface{},
) (*Lock, error) {
	zlog := d.zlog.With().Str("id", id).Logger()
	nowMilliseconds := time.Now().UnixNano() / int64(time.Millisecond)
	existingLock, err := d.getLock(ctx, id)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to get lock")
		return nil, err
	}
	if existingLock != nil {
		zlog.Debug().Interface("existingLock", existingLock).Msg("lock is already acquired")
		if !existingLock.IsExpired(nowMilliseconds) {
			zlog.Debug().Msg("lock is already acquired and not expired")
			return existingLock, LockCurrentlyUnavailableError{}
		}

		zlog.Debug().Msg("lock is already acquired but expired")
		newLock, err := d.updateExistingLock(ctx, *existingLock, data, nowMilliseconds)
		if err != nil {
			// Lock is acquired, expired, and when we tried to get it we got a conditional error, meaning we lost
			// the lease to someone else. We need to evict the lock from our cache and return an error.
			if err == LockConditionalUpdateFailedError {
				zlog.Debug().Msg("lock is already acquired but expired and conditional check failed")
				d.mu.Lock()
				delete(d.locks, id)
				d.mu.Unlock()
				return nil, LockCurrentlyUnavailableError{}
			}

			zlog.Error().Err(err).Msg("failed to update existing lock")
			return nil, err
		}

		return newLock, nil
	}

	zlog.Debug().Msg("lock is not acquired")
	lock, err := d.putNewLock(ctx, id, data, nowMilliseconds)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to put new lock")
		return nil, err
	}

	zlog.Info().Interface("lock", lock).Msg("acquired lock")
	return lock, nil
}

func (d *DynamoDBLockClient) Heartbeat(
	ctx context.Context,
	id string,
	maybeNewData *interface{},
) error {
	zlog := d.zlog.With().Str("id", id).Logger()
	zlog.Debug().Msg("heartbeat")

	existingLock, ok := d.getLocalLock(id)
	if !ok {
		zlog.Debug().Msg("lock is not locally acquired")
		return LockNotFoundError
	}

	// if the existing lock was created more than 5 minutes ago, then just leave it alone
	const abandonLockAfterMilliseconds = 5 * 60 * 1000
	if existingLock.CreatedAtMilliseconds < time.Now().UnixNano()/int64(time.Millisecond)-abandonLockAfterMilliseconds {
		zlog.Debug().Msg("lock is more than 1 minute old, abandoning it")
		return LockAbandonedError
	}

	var newData interface{}
	if maybeNewData != nil {
		newData = *maybeNewData
	} else {
		newData = existingLock.Data
	}

	var resultError multierror.Error
	nowMilliseconds := time.Now().UnixNano() / int64(time.Millisecond)
	_, err := d.updateExistingLock(ctx, existingLock, newData, nowMilliseconds)
	if err != nil {
		// Lock is acquired, expired, and when we tried to get it we got a conditional error, meaning we lost
		// the lease to someone else. We need to evict the lock from our cache and return an error.
		if err == LockConditionalUpdateFailedError {
			zlog.Debug().Msg("lock is already acquired but expired and conditional check failed")
			d.mu.Lock()
			delete(d.locks, id)
			d.mu.Unlock()
			return LockCurrentlyUnavailableError{}
		}

		zlog.Error().Err(err).Msg("failed to update existing lock")
		resultError = *multierror.Append(&resultError, err, LockHeartbeatFailedError)
	}

	return resultError.ErrorOrNil()
}

func (d *DynamoDBLockClient) Release(ctx context.Context, id string) error {
	zlog := d.zlog.With().Str("id", id).Logger()
	zlog.Debug().Msg("releasing lock")

	existingLock, ok := d.getLocalLock(id)
	if !ok {
		return LockNotFoundError
	}

	var resultError multierror.Error
	err := d.releaseLock(ctx, existingLock, &zlog)
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to delete lock")
		resultError = *multierror.Append(&resultError, err, LockReleaseFailedError)
	}

	return resultError.ErrorOrNil()
}

// getLock returns the lock with the given ID. If the lock is not found, then it returns nil.
func (d *DynamoDBLockClient) getLock(
	ctx context.Context,
	id string,
) (*Lock, error) {
	zlog := d.zlog.With().Str("id", id).Logger()
	zlog.Debug().Msg("getting lock")

	resp, err := d.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &d.TableName,
		Key: map[string]dynamodbtypes.AttributeValue{
			"LockID": &dynamodbtypes.AttributeValueMemberS{
				Value: id,
			},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		zlog.Error().Err(err).Msg("failed to get lock")
		return nil, err
	}
	zlog.Debug().Interface("resp", resp).Msg("got lock")

	if resp.Item == nil {
		zlog.Debug().Msg("lock not found")
		return nil, nil
	}

	owner := resp.Item["Owner"].(*dynamodbtypes.AttributeValueMemberS).Value
	zlog.Debug().Str("owner", owner).Msg("got owner")

	leaseDurationMilliseconds, err := strconv.Atoi(resp.Item["LeaseDurationMilliseconds"].(*dynamodbtypes.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse lease duration")
		return nil, err
	}
	zlog.Debug().Int("leaseDurationMilliseconds", leaseDurationMilliseconds).Msg("got lease duration")

	lastUpdatedTimeMilliseconds, err := strconv.Atoi(resp.Item["LastUpdatedTimeMilliseconds"].(*dynamodbtypes.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse last updated time")
		return nil, err
	}
	zlog.Debug().Int("lastUpdatedTimeMilliseconds", lastUpdatedTimeMilliseconds).Msg("got last updated time")

	recordVersionNumber := resp.Item["RecordVersionNumber"].(*dynamodbtypes.AttributeValueMemberS).Value
	zlog.Debug().Str("recordVersionNumber", recordVersionNumber).Msg("got record version number")

	shard, err := strconv.Atoi(resp.Item["Shard"].(*dynamodbtypes.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse shard")
		return nil, err
	}

	ttl, err := strconv.Atoi(resp.Item["TTL"].(*dynamodbtypes.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse TTL")
		return nil, err
	}
	zlog.Debug().Int("ttl", ttl).Msg("got TTL")

	createdAtMilliseconds, err := strconv.Atoi(resp.Item["CreatedAtMilliseconds"].(*dynamodbtypes.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse createdAt")
		return nil, err
	}

	dataSerialized := resp.Item["Data"].(*dynamodbtypes.AttributeValueMemberB).Value
	zlog.Debug().Str("dataSerialized", string(dataSerialized)).Msg("got data")

	var data interface{}
	err = json.Unmarshal(dataSerialized, &data)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to deserialize data")
		return nil, err
	}
	zlog.Debug().Interface("data", data).Msg("got deserialized data")

	newLock := PtrToLock(NewLock(
		id,
		owner,
		int64(leaseDurationMilliseconds),
		int64(lastUpdatedTimeMilliseconds),
		recordVersionNumber,
		int64(shard),
		int64(ttl),
		int64(createdAtMilliseconds),
		data,
	))
	zlog.Debug().Interface("lock", newLock).Msg("returning lock")

	d.mu.Lock()
	defer d.mu.Unlock()
	d.locks[id] = *newLock

	return newLock, nil
}

func (d *DynamoDBLockClient) updateExistingLock(
	ctx context.Context,
	existingLock Lock,
	newData interface{},
	nowMilliseconds int64,
) (*Lock, error) {
	zlog := d.zlog.With().Str("id", existingLock.ID).Logger()
	leaseDurationMilliseconds := int64(d.Config.LeaseDurationSeconds) * int64(time.Second) / int64(time.Millisecond)
	newRecordVersionNumber, err := uuid.NewV7()
	if err != nil {
		zlog.Error().Err(err).Msg("failed to generate record version number")
		return nil, err
	}
	newTtl := nowMilliseconds/1000 + 10*leaseDurationMilliseconds/1000

	newLock := NewLock(
		existingLock.ID,
		d.Config.Owner,
		leaseDurationMilliseconds,
		nowMilliseconds,
		newRecordVersionNumber.String(),
		existingLock.Shard,
		newTtl,
		existingLock.CreatedAtMilliseconds,
		newData,
	)
	item, err := lockToDynamoDBAttributeValues(newLock)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to convert lock to dynamodb attribute values")
		return nil, err
	}

	conditionSameRecordVersionNumber := expression.Name("RecordVersionNumber").Equal(expression.Value(existingLock.RecordVersionNumber))
	conditionSameOwner := expression.Name("Owner").Equal(expression.Value(existingLock.Owner))
	conditionDifferentOwner := expression.Name("Owner").NotEqual(expression.Value(existingLock.Owner))
	conditionExpired := expression.Name("LastUpdatedTimeMilliseconds").LessThan(expression.Value(nowMilliseconds - leaseDurationMilliseconds))
	condition := conditionSameRecordVersionNumber.And(conditionSameOwner.Or(conditionDifferentOwner.And(conditionExpired)))
	builder := expression.NewBuilder()
	builder = builder.WithCondition(condition)
	expr, err := builder.Build()
	if err != nil {
		zlog.Error().Err(err).Msg("failed to build expression")
		return nil, err
	}

	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 &d.TableName,
		Item:                      item,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		// If this is a ConditionalCheckFailedException, then the lock was not updated because the condition was not met.
		// This is an expected error and means we've lost the lease.
		var ccfe *dynamodbtypes.ConditionalCheckFailedException
		if errors.As(err, &ccfe) {
			zlog.Debug().Err(err).Msg("failed to update lock because condition was not met")
			return nil, LockConditionalUpdateFailedError
		}

		zlog.Error().Err(err).Msg("failed to update lock")
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.locks[existingLock.ID] = newLock

	return &newLock, nil
}

func (d *DynamoDBLockClient) putNewLock(
	ctx context.Context,
	id string,
	data interface{},
	nowMilliseconds int64,
) (*Lock, error) {
	leaseDurationMilliseconds := int64(d.Config.LeaseDurationSeconds) * int64(time.Second) / int64(time.Millisecond)
	recordVersionNumber, err := uuid.NewV7()
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to generate record version number")
		return nil, err
	}
	shard := rand.Intn(d.Config.MaxShards)
	ttl := nowMilliseconds/1000 + int64(d.Config.LeaseDurationSeconds)*10

	lock := NewLock(
		id,
		d.Config.Owner,
		leaseDurationMilliseconds,
		nowMilliseconds,
		recordVersionNumber.String(),
		int64(shard),
		ttl,
		nowMilliseconds,
		data,
	)
	item, err := lockToDynamoDBAttributeValues(lock)
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to convert lock to DynamoDB attribute values")
		return nil, err
	}

	builder := expression.NewBuilder()
	builder = builder.WithCondition(expression.Name("LockID").AttributeNotExists())
	expr, err := builder.Build()
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to build expression")
		return nil, err
	}

	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:                 &d.TableName,
		Item:                      item,
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to put lock")
		return nil, err
	}

	d.mu.Lock()
	defer d.mu.Unlock()
	d.locks[id] = lock

	return PtrToLock(lock), nil
}

func (d *DynamoDBLockClient) releaseLock(
	ctx context.Context,
	existingLock Lock,
	zlog *zerolog.Logger,
) error {
	// First release the lock locally. If the remote release fails, the lease will eventually expire and the lock will
	// be available again.
	d.mu.Lock()
	delete(d.locks, existingLock.ID)
	d.mu.Unlock()

	conditionSameRecordVersionNumber := expression.Name("RecordVersionNumber").Equal(expression.Value(existingLock.RecordVersionNumber))
	conditionSameOwner := expression.Name("Owner").Equal(expression.Value(d.Config.Owner))
	condition := conditionSameRecordVersionNumber.And(conditionSameOwner)
	builder := expression.NewBuilder()
	builder = builder.WithCondition(condition)
	expr, err := builder.Build()
	if err != nil {
		d.zlog.Error().Err(err).Msg("failed to build expression")
		return err
	}

	// Delete item from table
	_, err = d.Client.DeleteItem(ctx, &dynamodb.DeleteItemInput{
		TableName: &d.TableName,
		Key: map[string]dynamodbtypes.AttributeValue{
			"LockID": &dynamodbtypes.AttributeValueMemberS{Value: existingLock.ID},
		},
		ConditionExpression:       expr.Condition(),
		ExpressionAttributeNames:  expr.Names(),
		ExpressionAttributeValues: expr.Values(),
	})
	if err != nil {
		zlog.Error().Err(err).Msg("failed to release lock")
		return err
	}

	return nil
}

func (d *DynamoDBLockClient) getLocalLock(id string) (Lock, bool) {
	d.mu.Lock()
	defer d.mu.Unlock()

	lock, ok := d.locks[id]
	d.zlog.Debug().Str("id", id).Interface("lock", lock).Bool("ok", ok).Msg("getLocalLock exit")
	return lock, ok
}

func lockToDynamoDBAttributeValues(lock Lock) (map[string]dynamodbtypes.AttributeValue, error) {
	serializedData, err := json.Marshal(lock.Data)
	if err != nil {
		return nil, err
	}
	if err != nil {
		return nil, err
	}
	return map[string]dynamodbtypes.AttributeValue{
		"LockID": &dynamodbtypes.AttributeValueMemberS{
			Value: lock.ID,
		},
		"Owner": &dynamodbtypes.AttributeValueMemberS{
			Value: lock.Owner,
		},
		"LeaseDurationMilliseconds": &dynamodbtypes.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.LeaseDurationMilliseconds)),
		},
		"LastUpdatedTimeMilliseconds": &dynamodbtypes.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.LastUpdatedTimeMilliseconds)),
		},
		"RecordVersionNumber": &dynamodbtypes.AttributeValueMemberS{
			Value: lock.RecordVersionNumber,
		},
		"Data": &dynamodbtypes.AttributeValueMemberB{
			Value: serializedData,
		},
		"Shard": &dynamodbtypes.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.Shard)),
		},
		"TTL": &dynamodbtypes.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.TTLEpochSeconds)),
		},
		"CreatedAtMilliseconds": &dynamodbtypes.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.CreatedAtMilliseconds)),
		},
	}, nil
}
