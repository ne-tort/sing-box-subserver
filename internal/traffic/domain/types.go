package domain

import "time"

// SubjectKind classifies a registered subject.
type SubjectKind string

const (
	KindDataplaneUser      SubjectKind = "dataplane_user"
	KindControlplaneUser   SubjectKind = "controlplane_user"
	KindInboundAggregate SubjectKind = "inbound_aggregate"
	KindAnonymous          SubjectKind = "anonymous"
)

// SeriesType is a persisted counter family.
type SeriesType string

const (
	SeriesDataplaneUser SeriesType = "dataplane_user"
	SeriesInbound       SeriesType = "inbound"
	SeriesOutbound      SeriesType = "outbound"
	SeriesSubject       SeriesType = "subject"
)

// Subject is a consumer-defined identity aggregated from dataplane keys.
type Subject struct {
	ID            string            `json:"subject_id"`
	Kinds         []SubjectKind     `json:"kinds,omitempty"`
	DataplaneKeys []string          `json:"dataplane_keys,omitempty"`
	Labels        map[string]string `json:"labels,omitempty"`
}

// Manifest is a batch of subjects from one consumer.
type Manifest struct {
	Consumer  string    `json:"consumer,omitempty"`
	Subjects  []Subject `json:"subjects"`
	UpdatedAt time.Time `json:"updated_at,omitempty"`
}

// SpeedLimit is per-dataplane-key bandwidth in bytes/sec. Zero = unlimited.
type SpeedLimit struct {
	UpBytesPerSec   int64 `json:"up_bytes_per_sec"`
	DownBytesPerSec int64 `json:"down_bytes_per_sec"`
}

// Sample is one flush delta for a series key.
type Sample struct {
	At         time.Time  `json:"ts"`
	SeriesType SeriesType `json:"series_type"`
	Key        string     `json:"key"`
	Up         int64      `json:"up"`
	Down       int64      `json:"down"`
}

// CounterTotal is a cumulative counter.
type CounterTotal struct {
	SeriesType SeriesType `json:"series_type"`
	Key        string     `json:"key"`
	Up         int64      `json:"up"`
	Down       int64      `json:"down"`
}

// Usage is aggregated up+down for a subject or key.
type Usage struct {
	SubjectID string `json:"subject_id,omitempty"`
	Key       string `json:"key,omitempty"`
	Up        int64  `json:"up"`
	Down      int64  `json:"down"`
	Total     int64  `json:"total"`
}
