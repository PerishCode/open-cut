package repository

import (
	"context"

	"github.com/PerishCode/open-cut/apps/api/model"
)

type Health interface {
	Read(context.Context) (model.Health, error)
}

type StaticHealth struct{}

func (StaticHealth) Read(context.Context) (model.Health, error) {
	return model.Health{OK: true, Service: "api"}, nil
}
