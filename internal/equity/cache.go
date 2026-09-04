package equity

import (
	"sort"
	"strings"
	"sync"

	"gitlab.com/smoothsics/ainp/internal/poker"
)

type resultCache struct {
	mu       sync.RWMutex
	capacity int
	entries  map[string]Result
	keys     []string
	next     int
}

func newResultCache(capacity int) *resultCache {
	return &resultCache{capacity: capacity, entries: make(map[string]Result, capacity), keys: make([]string, 0, capacity)}
}

func (c *resultCache) Get(key string) (Result, bool) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	result, ok := c.entries[key]
	return result, ok
}

func (c *resultCache) Set(key string, result Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, exists := c.entries[key]; exists {
		c.entries[key] = result
		return
	}
	if len(c.keys) < c.capacity {
		c.keys = append(c.keys, key)
	} else {
		delete(c.entries, c.keys[c.next])
		c.keys[c.next] = key
		c.next = (c.next + 1) % c.capacity
	}
	c.entries[key] = result
}

func exactCacheKey(req Request) string {
	game := req.Game
	if game == "" {
		game = GameNLH
	}
	parts := []string{"G:" + string(game), "H:" + sortedCards(req.Hero), "B:" + sortedCards(req.Board), "D:" + sortedCards(req.Dead)}
	opponents := make([]string, len(req.Opponents))
	for i, cards := range req.Opponents {
		if len(cards) == 0 {
			opponents[i] = "--"
		} else {
			opponents[i] = sortedCards(cards)
		}
	}
	sort.Strings(opponents)
	parts = append(parts, "O:"+strings.Join(opponents, ","))
	return strings.Join(parts, "|")
}

func sortedCards(cards []poker.Card) string {
	copyCards := append([]poker.Card(nil), cards...)
	sort.Slice(copyCards, func(i, j int) bool { return copyCards[i] < copyCards[j] })
	return poker.CardsString(copyCards)
}
