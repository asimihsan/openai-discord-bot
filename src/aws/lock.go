package aws

import (
	"bytes"
	"context"
	"encoding/json"
	"strings"
)

type Lock[T any] struct {
	Id                          string
	Owner                       string
	LeaseDurationMilliseconds   int64
	LastUpdatedTimeMilliseconds int64
	RecordVersionNumber         string
	Data                        T
}

func (l *Lock[T]) IsExpired(nowMilliseconds int64) bool {
	return nowMilliseconds-l.LastUpdatedTimeMilliseconds > l.LeaseDurationMilliseconds
}

type LockClient[T any] interface {
	Acquire(ctx context.Context, id string, data T) (Lock[T], error)
	Heartbeat(ctx context.Context, id string, lock Lock[T]) error
	Release(ctx context.Context, id string, lock Lock[T]) error
}

func NewLock[T any](
	Id string,
	Owner string,
	LeaseDurationMilliseconds int64,
	LastUpdatedTimeMilliseconds int64,
	RecordVersionNumber string,
	Data T,
) Lock[T] {
	return Lock[T]{
		Id:                          Id,
		Owner:                       Owner,
		LeaseDurationMilliseconds:   LeaseDurationMilliseconds,
		LastUpdatedTimeMilliseconds: LastUpdatedTimeMilliseconds,
		RecordVersionNumber:         RecordVersionNumber,
		Data:                        Data,
	}
}

func Ptr[T any](t T) *T {
	return &t
}

func Serialize[T any](t T) (string, error) {
	buffer := &bytes.Buffer{}
	encoder := json.NewEncoder(buffer)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(t)
	return strings.TrimRight(buffer.String(), "\n"), err
}

func Deserialize[T any](s string) (T, error) {
	decoder := json.NewDecoder(strings.NewReader(s))
	var t T
	err := decoder.Decode(&t)
	return t, err
}
