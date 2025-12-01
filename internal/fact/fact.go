package fact

import (
	"context"
	"errors"
	"io"
)

var (
	ErrFactorizationCancelled = errors.New("cancelled")
	ErrWriterInteraction      = errors.New("writer interaction")
)

type Factorizer interface {
	Factorize(ctx context.Context, numbers []int, writer io.Writer) error
}

type factorizerImpl struct{}

func New(opts ...FactorizeOption) (*factorizerImpl, error) {
	panic("not implemented")
}

type FactorizeOption func(*factorizerImpl)

func WithFactorizationWorkers(workers int) FactorizeOption {
	panic("not implemented")
}

func WithWriteWorkers(workers int) FactorizeOption {
	panic("not implemented")
}

func (f *factorizerImpl) Factorize(
	_ context.Context,
	_ []int,
	_ io.Writer,
) error {
	panic("not implemented")
}
