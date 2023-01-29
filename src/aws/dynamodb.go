package aws

import (
	"context"
	"errors"
	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/service/dynamodb"
	dynamodb_types "github.com/aws/aws-sdk-go-v2/service/dynamodb/types"
	"github.com/gofrs/uuid"
	"github.com/rs/zerolog"
	"strconv"
	"time"
)

var (
	LockCurrentlyUnavailableError = errors.New("lock is currently unavailable")
)

type DynamoDBLockConfig struct {
	Owner                string
	MaxShards            int
	LeaseDurationSeconds int
}

type DynamoDBLockClient[T any] struct {
	Client    *dynamodb.Client
	TableName string
	Config    DynamoDBLockConfig
}

func NewDynamoDBClient(region string) (*dynamodb.Client, error) {
	cfg, err := config.LoadDefaultConfig(context.TODO(),
		config.WithRegion(region),
	)
	if err != nil {
		return nil, err
	}
	return dynamodb.NewFromConfig(cfg), nil
}

func NewDynamoDBLockClient[T any](
	tableName string,
	region string,
	config DynamoDBLockConfig,
) (*DynamoDBLockClient[T], error) {
	client, err := NewDynamoDBClient(region)
	if err != nil {
		return nil, err
	}

	return &DynamoDBLockClient[T]{
		Client:    client,
		TableName: tableName,
		Config:    config,
	}, nil
}

func (d *DynamoDBLockClient[T]) Acquire(
	ctx context.Context,
	id string,
	data T,
	zlog *zerolog.Logger,
) (*Lock[T], error) {
	// The DynamoDB table has a hash key LockID string.
	// First try to get the lock by LockID.
	// If the lock is not acquired, then try to acquire it by setting the lock with the LockExpirationTime
	// field set to the current time + the Lock duration.
	// If the lock is acquired, then return the lock.
	// If the lock is not acquired, then return an error.

	// Put the lock in the table if 1) the lock is not in the table or 2) the lock is in the table and the
	// LockExpirationTime is less than the current time.

	nowMilliseconds := time.Now().UnixNano() / int64(time.Millisecond)
	existingLock, err := d.getLock(ctx, id, zlog)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to get lock")
		return nil, err
	}
	if existingLock != nil {
		if !existingLock.IsExpired(nowMilliseconds) {
			zlog.Debug().Msg("lock is already acquired and not expired")
			return existingLock, LockCurrentlyUnavailableError
		}
		zlog.Debug().Msg("lock is already acquired but expired")
	}

	zlog.Debug().Msg("lock is not acquired")
}

func (d *DynamoDBLockClient[T]) Heartbeat(ctx context.Context, id string, lock Lock[T]) error {
	panic("implement me")
}

func (d *DynamoDBLockClient[T]) Release(ctx context.Context, id string, lock Lock[T]) error {
	panic("implement me")
}

// getLock returns the lock with the given ID. If the lock is not found, then it returns nil.
func (d *DynamoDBLockClient[T]) getLock(
	ctx context.Context,
	id string,
	zlog *zerolog.Logger,
) (*Lock[T], error) {
	resp, err := d.Client.GetItem(ctx, &dynamodb.GetItemInput{
		TableName: &d.TableName,
		Key: map[string]dynamodb_types.AttributeValue{
			"LockID": &dynamodb_types.AttributeValueMemberS{
				Value: id,
			},
		},
		ConsistentRead: aws.Bool(true),
	})
	if err != nil {
		zlog.Error().Err(err).Msg("failed to get lock")
		return nil, err
	}
	if resp.Item == nil {
		zlog.Debug().Msg("lock not found")
		return nil, nil
	}

	owner := resp.Item["Owner"].(*dynamodb_types.AttributeValueMemberS).Value
	leaseDurationMilliseconds, err := strconv.Atoi(resp.Item["LeaseDurationMilliseconds"].(*dynamodb_types.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse lease duration")
		return nil, err
	}
	lastUpdatedTimeMilliseconds, err := strconv.Atoi(resp.Item["LastUpdatedTimeMilliseconds"].(*dynamodb_types.AttributeValueMemberN).Value)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to parse last updated time")
		return nil, err
	}
	recordVersionNumber := resp.Item["RecordVersionNumber"].(*dynamodb_types.AttributeValueMemberS).Value
	dataSerialized := resp.Item["Data"].(*dynamodb_types.AttributeValueMemberS).Value
	data, err := Deserialize(dataSerialized)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to deserialize data")
		return nil, err
	}

	return Ptr(NewLock[T](
		id,
		owner,
		int64(leaseDurationMilliseconds),
		int64(lastUpdatedTimeMilliseconds),
		recordVersionNumber,
		data,
	)), nil
}

func (d *DynamoDBLockClient[T]) putNewLock(
	ctx context.Context,
	id string,
	data T,
	zlog *zerolog.Logger,
) error {
	nowMilliseconds := time.Now().UnixNano() / int64(time.Millisecond)
	leaseDurationMilliseconds := int64(d.Config.LeaseDurationSeconds) * int64(time.Second) / int64(time.Millisecond)
	serializedData, err := Serialize(data)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to serialize data")
		return err
	}
	recordVersionNumber, err := uuid.NewV6()
	if err != nil {
		zlog.Error().Err(err).Msg("failed to generate record version number")
		return err
	}

	// Put the lock into the table, but use a condition expression to ensure that the lock is not already in the table.
	lock := NewLock[T](
		id,
		d.Config.Owner,
		leaseDurationMilliseconds,
		nowMilliseconds,
		recordVersionNumber.String(),
		data,
	)
	item, err := lockToDynamoDBAttributeValues(lock)
	if err != nil {
		zlog.Error().Err(err).Msg("failed to convert lock to DynamoDB attribute values")
		return err
	}

	_, err = d.Client.PutItem(ctx, &dynamodb.PutItemInput{
		TableName:           &d.TableName,
		Item:                item,
		ConditionExpression: aws.String("attribute_not_exists(LockID)"),
	})
}

func lockToDynamoDBAttributeValues[T any](lock Lock[T]) (map[string]dynamodb_types.AttributeValue, error) {
	serializedData, err := Serialize(lock.Data)
	if err != nil {
		return nil, err
	}
	return map[string]dynamodb_types.AttributeValue{
		"LockID": &dynamodb_types.AttributeValueMemberS{
			Value: lock.ID,
		},
		"Owner": &dynamodb_types.AttributeValueMemberS{
			Value: lock.Owner,
		},
		"LeaseDurationMilliseconds": &dynamodb_types.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.LeaseDurationMilliseconds)),
		},
		"LastUpdatedTimeMilliseconds": &dynamodb_types.AttributeValueMemberN{
			Value: strconv.Itoa(int(lock.LastUpdatedTimeMilliseconds)),
		},
		"RecordVersionNumber": &dynamodb_types.AttributeValueMemberS{
			Value: lock.RecordVersionNumber,
		},
		"Data": &dynamodb_types.AttributeValueMemberS{
			Value: serializedData,
		},
	}, nil
}
