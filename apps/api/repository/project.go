package repository

import (
	"context"
	"sort"
	"sync"

	"github.com/PerishCode/open-cut/apps/api/model"
)

type Projects interface {
	Snapshot(context.Context) (model.ProjectSnapshot, error)
	Put(context.Context, model.Project) (model.ProjectUpserted, error)
}

type MemoryProjects struct {
	mu       sync.RWMutex
	projects map[string]model.Project
	revision uint64
}

func NewMemoryProjects(seed ...model.Project) *MemoryProjects {
	projects := make(map[string]model.Project, len(seed))
	for _, project := range seed {
		projects[project.ID] = project
	}
	return &MemoryProjects{projects: projects}
}

func (repository *MemoryProjects) Snapshot(context.Context) (model.ProjectSnapshot, error) {
	repository.mu.RLock()
	defer repository.mu.RUnlock()
	projects := make([]model.Project, 0, len(repository.projects))
	for _, project := range repository.projects {
		projects = append(projects, project)
	}
	sort.Slice(projects, func(i, j int) bool { return projects[i].ID < projects[j].ID })
	return model.ProjectSnapshot{Revision: repository.revision, Projects: projects}, nil
}

func (repository *MemoryProjects) Put(_ context.Context, project model.Project) (model.ProjectUpserted, error) {
	repository.mu.Lock()
	defer repository.mu.Unlock()
	repository.projects[project.ID] = project
	repository.revision++
	return model.ProjectUpserted{Revision: repository.revision, Project: project}, nil
}
