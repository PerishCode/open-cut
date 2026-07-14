package service

import (
	"context"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/repository"
)

type Health interface {
	Get(context.Context) (model.Health, error)
}

type health struct {
	repository repository.Health
}

func NewHealth(source repository.Health) Health {
	return health{repository: source}
}

func (current health) Get(ctx context.Context) (model.Health, error) {
	return current.repository.Read(ctx)
}
