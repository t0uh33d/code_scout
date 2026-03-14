package ports

import "context"

// TransactionManager abstracts database transaction lifecycle.
// The fn callback receives a context that carries the transaction;
// repositories detect it and use the transactional connection automatically.
type TransactionManager interface {
	WithTransaction(ctx context.Context, fn func(ctx context.Context) error) error
}
