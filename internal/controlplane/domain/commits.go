//go:build with_controlplane

package domain

import "time"

// BlockHead is the content-addressed tip of one configure block.
type BlockHead struct {
	SHA256    string    `json:"sha256"`
	UpdatedAt time.Time `json:"updated_at"`
}

// HeadsFile is controlplane/heads.json.
type HeadsFile struct {
	Blocks            map[string]BlockHead `json:"blocks"`
	MaterializeSHA256 string               `json:"materialize_sha256,omitempty"`
}

// CommitMeta is controlplane/commits/_meta.json.
type CommitMeta struct {
	PendingID string   `json:"pending_id,omitempty"`
	Recent    []string `json:"recent,omitempty"`
}

// CommitStatus lifecycle for async apply.
type CommitStatus string

const (
	CommitAccepted  CommitStatus = "accepted"
	CommitApplying  CommitStatus = "applying"
	CommitApplied   CommitStatus = "applied"
	CommitFailed    CommitStatus = "failed"
	CommitConflict  CommitStatus = "conflict"
)

// CommitBlock is one block payload inside a commit.
type CommitBlock struct {
	SHA256 string         `json:"sha256,omitempty"`
	Body   map[string]any `json:"body"`
}

// CommitBase is optional If-Match against heads / materialize.
type CommitBase struct {
	MaterializeSHA256 string            `json:"materialize_sha256,omitempty"`
	Blocks            map[string]string `json:"blocks,omitempty"`
}

// CommitResult is filled when apply finishes.
type CommitResult struct {
	MaterializeSHA256  string     `json:"materialize_sha256,omitempty"`
	SupervisorRevision uint64     `json:"supervisor_revision,omitempty"`
	Error              string     `json:"error,omitempty"`
	ErrorCode          string     `json:"error_code,omitempty"`
	FinishedAt         *time.Time `json:"finished_at,omitempty"`
}

// Commit is controlplane/commits/<id>.json.
type Commit struct {
	ID             string                 `json:"id"`
	Status         CommitStatus           `json:"status"`
	CreatedAt      time.Time              `json:"created_at"`
	Source         string                 `json:"source,omitempty"`
	Base           *CommitBase            `json:"base,omitempty"`
	Blocks         map[string]CommitBlock `json:"blocks"`
	PrevActiveSets []string               `json:"prev_active_sets,omitempty"`
	Result         *CommitResult          `json:"result,omitempty"`
}

const MaxRecentCommits = 32
