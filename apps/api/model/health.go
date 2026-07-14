package model

// Health is the public product API health representation.
type Health struct {
	OK      bool   `json:"ok" doc:"Whether the API is ready"`
	Service string `json:"service" doc:"Service reporting health" enum:"api"`
}
