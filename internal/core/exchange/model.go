package exchange

import "time"

const (
	DefaultCapacity     = 100
	DefaultMaxBodyBytes = 64 << 10
)

type Record struct {
	ID              uint64
	StartedAt       time.Time
	CompletedAt     time.Time
	DurationMillis  int64
	Method          string
	Host            string
	URL             string
	TargetURL       string
	StatusCode      int
	RequestHeaders  map[string][]string
	ResponseHeaders map[string][]string
	RequestBody     Body
	ResponseBody    Body
}

type Body struct {
	Text          string
	Base64        string
	Bytes         int64
	CapturedBytes int
	Truncated     bool
}

type Settings struct {
	Enabled      bool
	Capacity     int
	MaxBodyBytes int64
}

type Snapshot struct {
	Settings Settings
	Total    uint64
	Items    []Record
}

type Capture struct {
	ID           uint64
	MaxBodyBytes int64
}
