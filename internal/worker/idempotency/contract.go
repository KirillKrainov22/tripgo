//go:generate go tool mockgen -source=${GOFILE} -destination=mocks_test.go -package=${GOPACKAGE}_test
package idempotency

import "context"

type keyRemover interface {
	DeleteExpired(ctx context.Context) (int64, error)
}
