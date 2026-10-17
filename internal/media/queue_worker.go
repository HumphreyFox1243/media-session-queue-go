package media

import (
	"errors"
	"sync"
	"time"
)

type Asset struct {
	ID        string    `json:"id"`
	CreatorID string    `json:"creator_id"`
	Title     string    `json:"title"`
	Source    string    `json:"source"`
	State     string    `json:"state"`
	CreatedAt time.Time `json:"created_at"`
	Delivery  string    `json:"delivery,omitempty"`
}

type Queue struct {
	mu     sync.Mutex
	assets map[string]Asset
	jobs   chan string
}

func NewQueue(depth int) *Queue {
	return &Queue{assets: make(map[string]Asset), jobs: make(chan string, depth)}
}

func (q *Queue) Ingest(asset Asset) (Asset, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if _, exists := q.assets[asset.ID]; exists {
		return Asset{}, errors.New("asset id already exists")
	}
	asset.State = "queued"
	asset.CreatedAt = time.Now().UTC()
	q.assets[asset.ID] = asset
	q.jobs <- asset.ID
	return asset, nil
}

func (q *Queue) ProcessNext() (Asset, bool) {
	select {
	case id := <-q.jobs:
		q.mu.Lock()
		defer q.mu.Unlock()
		asset := q.assets[id]
		asset.State = "ready"
		asset.Delivery = "/creator/assets/" + asset.ID
		q.assets[id] = asset
		return asset, true
	default:
		return Asset{}, false
	}
}

func (q *Queue) Get(id, creatorID string) (Asset, bool) {
	q.mu.Lock()
	defer q.mu.Unlock()
	asset, ok := q.assets[id]
	return asset, ok && asset.CreatorID == creatorID
}
