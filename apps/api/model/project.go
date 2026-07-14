package model

// Project is the smallest product resource used to prove the application data plane.
type Project struct {
	ID          string `json:"id" doc:"Stable project identifier" minLength:"1" maxLength:"128"`
	Name        string `json:"name" doc:"Human-readable project name" minLength:"1" maxLength:"200"`
	Description string `json:"description" doc:"Project description" maxLength:"2000"`
}

// ProjectWrite contains the client-controlled fields of a project mutation.
type ProjectWrite struct {
	Name        string `json:"name" doc:"Human-readable project name" minLength:"1" maxLength:"200"`
	Description string `json:"description" doc:"Project description" maxLength:"2000"`
}

// ProjectSnapshot is an atomic read model and the first event on every stream.
type ProjectSnapshot struct {
	Revision uint64    `json:"revision" doc:"Monotonic project state revision"`
	Projects []Project `json:"projects" doc:"Projects ordered by identifier"`
}

// ProjectUpserted is both the write receipt and the authoritative project delta.
type ProjectUpserted struct {
	Revision uint64  `json:"revision" doc:"Monotonic project state revision"`
	Project  Project `json:"project"`
}
