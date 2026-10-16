package service

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/redis/go-redis/v9"

	"github.com/ahmadfahrezi81/rail-canvas/api/internal/store"
)

const (
	// Rebuilt from Postgres at least this often, so any drift heals by itself.
	boardTTL      = 10 * time.Minute
	boardLockTTL  = 5 * time.Second
	boardLockWait = time.Second
)

// ver holds "pixel_id:color" per cell: the newest pixel Redis has seen there.

// setCell: compare-and-set one placement, after its commit. An older pixel never
// overwrites a newer one, whatever order the writes arrive in. The board byte is set
// only if the board exists: a missing board is rebuilt whole on the next read.
var setCell = redis.NewScript(`
local cur = redis.call('HGET', KEYS[2], ARGV[1])
if cur and tonumber(string.match(cur, '^(%d+)')) >= tonumber(ARGV[2]) then return 0 end
redis.call('HSET', KEYS[2], ARGV[1], ARGV[2] .. ':' .. ARGV[3])
if redis.call('TTL', KEYS[2]) < 0 then redis.call('EXPIRE', KEYS[2], ARGV[4]) end
if redis.call('EXISTS', KEYS[1]) == 1 then
  redis.call('BITFIELD', KEYS[1], 'SET', 'u8', '#' .. ARGV[1], ARGV[3])
end
return 1
`)

// rebuildBoard writes the board read from Postgres, merged with ver: per cell the newer
// pixel wins, and Postgres's newer ids go into ver. That covers a pixel committed after
// the Postgres read but announced before this write. ARGV: board, ttl, then cell, id,
// color for every painted cell Postgres returned.
var rebuildBoard = redis.NewScript(`
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
local fromDB = {}
for i = 3, #ARGV, 3 do
  local cell, id = ARGV[i], tonumber(ARGV[i + 1])
  fromDB[cell] = true
  local cur = redis.call('HGET', KEYS[2], cell)
  local curID, curColor
  if cur then curID, curColor = string.match(cur, '^(%d+):(%d+)$') end
  if curID and tonumber(curID) > id then
    redis.call('BITFIELD', KEYS[1], 'SET', 'u8', '#' .. cell, curColor)
  else
    redis.call('HSET', KEYS[2], cell, ARGV[i + 1] .. ':' .. ARGV[i + 2])
  end
end
local ver = redis.call('HGETALL', KEYS[2])
for i = 1, #ver, 2 do
  if not fromDB[ver[i]] then
    redis.call('BITFIELD', KEYS[1], 'SET', 'u8', '#' .. ver[i], string.match(ver[i + 1], ':(%d+)$'))
  end
end
redis.call('EXPIRE', KEYS[2], ARGV[2])
return 1
`)

// cachedBoard reads the board from Redis, rebuilding it on a miss. ok is false when
// Redis failed and the caller should read Postgres. Freshness: a Redis copy kept current
// by every placement, rebuilt from Postgres at least every 10 minutes.
func (s *Canvases) cachedBoard(ctx context.Context, spaceID, canvasID uuid.UUID) (board []byte, ok bool, err error) {
	r := s.redis
	if !r.Up() {
		return nil, false, nil
	}
	key := boardKey(spaceID, canvasID)
	if _, stale := r.stale.Load(key); stale {
		if err := r.c.Del(ctx, key).Err(); err != nil {
			r.Failed("board drop stale", err)
			return nil, false, nil
		}
		r.stale.Delete(key)
	}
	data, err := r.c.Get(ctx, key).Bytes()
	switch {
	case err == nil:
		r.boardHits.Add(1)
		return data, true, nil
	case !isMiss(err):
		r.Failed("board get", err)
		return nil, false, nil
	}
	r.boardMisses.Add(1)

	// Single-flight: one rebuild per canvas in this process, whatever the number of misses.
	v, err, _ := s.flight.Do(spaceID.String()+canvasID.String(), func() (any, error) {
		return s.rebuild(ctx, spaceID, canvasID)
	})
	if errors.Is(err, ErrNotFound) {
		return nil, true, err
	}
	if err != nil {
		r.Failed("board rebuild", err)
		return nil, false, nil
	}
	return v.([]byte), true, nil
}

// rebuild fills the cache from Postgres. Across API copies a short lock keeps it to one
// rebuild; a copy that did not get the lock waits briefly for the board, then gives up.
func (s *Canvases) rebuild(ctx context.Context, spaceID, canvasID uuid.UUID) ([]byte, error) {
	r := s.redis
	got, err := r.c.SetNX(ctx, lockKey(spaceID, canvasID), 1, boardLockTTL).Result()
	if err != nil {
		return nil, err
	}
	if !got {
		deadline := time.Now().Add(boardLockWait)
		for time.Now().Before(deadline) {
			time.Sleep(50 * time.Millisecond)
			if data, err := r.c.Get(ctx, boardKey(spaceID, canvasID)).Bytes(); err == nil {
				return data, nil
			}
		}
		return nil, errors.New("board rebuild by another copy did not finish in time")
	}
	defer r.c.Del(context.WithoutCancel(ctx), lockKey(spaceID, canvasID))

	s.scans.Add(1)
	var board []byte
	args := []any{nil, int(boardTTL.Seconds())}
	err = inTenant(ctx, s.pool, spaceID, func(q *store.Queries) error {
		c, err := q.GetCanvas(ctx, store.GetCanvasParams{ID: canvasID, SpaceID: spaceID})
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		}
		if err != nil {
			return err
		}
		cells, err := q.ListCanvasCells(ctx, store.ListCanvasCellsParams{CanvasID: canvasID, SpaceID: spaceID})
		if err != nil {
			return err
		}
		w := int(c.Width)
		board = make([]byte, w*int(c.Height))
		for _, cell := range cells {
			i := int(cell.Y)*w + int(cell.X)
			board[i] = byte(cell.Color)
			args = append(args, i, cell.PixelID, cell.Color)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	args[0] = board
	if err := rebuildBoard.Run(ctx, r.c, []string{boardKey(spaceID, canvasID), verKey(spaceID, canvasID)}, args...).Err(); err != nil {
		return nil, err
	}
	// Read back: the merge may have put a newer pixel in.
	return r.c.Get(ctx, boardKey(spaceID, canvasID)).Bytes()
}

// cacheCell records a committed placement. If Redis is down or fails, the board is
// marked stale and dropped as soon as Redis answers again (see Stats), so the next read
// rebuilds it; the 10-minute expiry is the last resort.
func (r *Redis) cacheCell(ctx context.Context, spaceID, canvasID uuid.UUID, width, x, y, color int, pixelID int64) {
	if r == nil {
		return
	}
	keys := []string{boardKey(spaceID, canvasID), verKey(spaceID, canvasID)}
	if !r.Up() {
		r.stale.Store(keys[0], struct{}{})
		return
	}
	cell := y*width + x
	err := setCell.Run(ctx, r.c, keys, strconv.Itoa(cell), pixelID, color, int(boardTTL.Seconds())).Err()
	if err != nil {
		r.Failed("board set", err)
		r.stale.Store(keys[0], struct{}{})
	}
}
