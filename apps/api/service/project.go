package service

import (
	"context"
	"sync"

	"github.com/PerishCode/open-cut/apps/api/model"
	"github.com/PerishCode/open-cut/apps/api/repository"
)

const projectSubscriberBuffer = 32

type ProjectSubscription struct {
	Snapshot model.ProjectSnapshot
	Events   <-chan model.ProjectUpserted
	Close    func()
}

type Projects interface {
	List(context.Context) (model.ProjectSnapshot, error)
	Put(context.Context, model.Project) (model.ProjectUpserted, error)
	Subscribe(context.Context) (ProjectSubscription, error)
}

type projects struct {
	mu          sync.Mutex
	repository  repository.Projects
	nextID      uint64
	subscribers map[uint64]chan model.ProjectUpserted
}

func NewProjects(source repository.Projects) Projects {
	return &projects{repository: source, subscribers: make(map[uint64]chan model.ProjectUpserted)}
}

func (current *projects) List(ctx context.Context) (model.ProjectSnapshot, error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	snapshot, err := current.repository.Snapshot(ctx)
	if err != nil {
		return model.ProjectSnapshot{}, err
	}
	return snapshot, nil
}

func (current *projects) Put(ctx context.Context, project model.Project) (model.ProjectUpserted, error) {
	current.mu.Lock()
	defer current.mu.Unlock()
	event, err := current.repository.Put(ctx, project)
	if err != nil {
		return model.ProjectUpserted{}, err
	}
	for id, subscriber := range current.subscribers {
		select {
		case subscriber <- event:
		default:
			close(subscriber)
			delete(current.subscribers, id)
		}
	}
	return event, nil
}

func (current *projects) Subscribe(ctx context.Context) (ProjectSubscription, error) {
	current.mu.Lock()
	snapshot, err := current.repository.Snapshot(ctx)
	if err != nil {
		current.mu.Unlock()
		return ProjectSubscription{}, err
	}
	current.nextID++
	id := current.nextID
	events := make(chan model.ProjectUpserted, projectSubscriberBuffer)
	current.subscribers[id] = events
	current.mu.Unlock()

	var once sync.Once
	closeSubscription := func() {
		once.Do(func() {
			current.mu.Lock()
			if active, ok := current.subscribers[id]; ok {
				delete(current.subscribers, id)
				close(active)
			}
			current.mu.Unlock()
		})
	}
	return ProjectSubscription{Snapshot: snapshot, Events: events, Close: closeSubscription}, nil
}
