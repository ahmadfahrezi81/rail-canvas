package service

import (
	"context"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
)

// The key outlives the longest cooldown a canvas can have (1 to 3600 seconds).
const cooldownKeyTTL = 3600

// claimCooldown keeps when the member last placed (Redis's clock, ms), not a key that
// expires with one cooldown: a member's turn is per space, but each canvas has its own
// cooldown, so the check compares with this canvas's. Returns ms left, or 0 when claimed.
var claimCooldownScript = redis.NewScript(`
local t = redis.call('TIME')
local now = tonumber(t[1]) * 1000 + math.floor(tonumber(t[2]) / 1000)
local last = redis.call('GET', KEYS[1])
if last then
  local left = tonumber(last) + tonumber(ARGV[1]) * 1000 - now
  if left > 0 then return left end
end
redis.call('SET', KEYS[1], now, 'EX', ARGV[2])
return 0
`)

// claimCooldown is the fast check before Postgres's claim, which stays the rule.
// ok is false when Redis failed: the caller goes straight to Postgres.
func (r *Redis) claimCooldown(ctx context.Context, spaceID, userID uuid.UUID, cooldownSeconds int32) (left time.Duration, ok bool) {
	if !r.Up() {
		return 0, false
	}
	ms, err := claimCooldownScript.Run(ctx, r.c, []string{cooldownKey(spaceID, userID)}, cooldownSeconds, cooldownKeyTTL).Int64()
	if err != nil {
		r.Failed("cooldown claim", err)
		return 0, false
	}
	return time.Duration(ms) * time.Millisecond, true
}

// releaseCooldown: the placement failed after Redis let it through; it must not cost a turn.
func (r *Redis) releaseCooldown(ctx context.Context, spaceID, userID uuid.UUID) {
	if err := r.c.Del(ctx, cooldownKey(spaceID, userID)).Err(); err != nil {
		r.Failed("cooldown release", err)
	}
}

// matchCooldown: Postgres said too soon although Redis did not (Redis forgot, e.g. a
// restart). Store Postgres's time, so Redis never blocks longer or shorter than the rule.
func (r *Redis) matchCooldown(ctx context.Context, spaceID, userID uuid.UUID, lastPlaced time.Time) {
	if err := r.c.Set(ctx, cooldownKey(spaceID, userID), lastPlaced.UnixMilli(), cooldownKeyTTL*time.Second).Err(); err != nil {
		r.Failed("cooldown match", err)
	}
}
