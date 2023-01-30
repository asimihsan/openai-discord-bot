package aws

import (
	"context"
)

type Marshaler interface {
	Marshal() ([]byte, error)
}

type Unmarshaler interface {
	Unmarshal([]byte) error
}

type LockDataType interface {
	Marshaler
	Unmarshaler
}

type Lock struct {
	ID                          string
	Owner                       string
	LeaseDurationMilliseconds   int64
	LastUpdatedTimeMilliseconds int64
	RecordVersionNumber         string
	Shard                       int64
	TTLEpochSeconds             int64
	Data                        LockDataType
}

func (l *Lock) IsExpired(nowMilliseconds int64) bool {
	return nowMilliseconds-l.LastUpdatedTimeMilliseconds > l.LeaseDurationMilliseconds
}

type LockClient interface {
	Acquire(ctx context.Context, id string, data LockDataType) (*Lock, error)
	Heartbeat(ctx context.Context, id string, maybeNewData *LockDataType) error
	Release(ctx context.Context, id string) error
	Close() error
}

func NewLock(
	ID string,
	Owner string,
	LeaseDurationMilliseconds int64,
	LastUpdatedTimeMilliseconds int64,
	RecordVersionNumber string,
	Shard int64,
	TTLEpochSeconds int64,
	Data LockDataType,
) Lock {
	return Lock{
		ID:                          ID,
		Owner:                       Owner,
		LeaseDurationMilliseconds:   LeaseDurationMilliseconds,
		LastUpdatedTimeMilliseconds: LastUpdatedTimeMilliseconds,
		RecordVersionNumber:         RecordVersionNumber,
		Shard:                       Shard,
		TTLEpochSeconds:             TTLEpochSeconds,
		Data:                        Data,
	}
}

func PtrToLock(l Lock) *Lock {
	return &l
}
