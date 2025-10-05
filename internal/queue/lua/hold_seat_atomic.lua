-- hold_seat_atomic.lua
-- Atomic seat hold operation: Check availability + Decrement inventory + Mark as HOLD
--
-- KEYS[1]: seat status hash (e.g., "seat:status:concert")
-- KEYS[2]: hold key (e.g., "hold:seat:A-12")
-- KEYS[3]: inventory counter (e.g., "inventory:concert")
--
-- ARGV[1]: seat_id
-- ARGV[2]: user_id
-- ARGV[3]: ttl (seconds)
--
-- Returns:
--   {1, remaining_count} on success (status=1, remaining_inventory)
--   {0, "SEAT_UNAVAILABLE"} if seat is not available (status=0, error_msg)
--   {0, "SOLD_OUT"} if inventory is exhausted

-- 1. Check seat availability
local status = redis.call('HGET', KEYS[1], ARGV[1])
if status ~= false and status ~= 'AVAILABLE' then
    return {0, 'SEAT_UNAVAILABLE'}
end

-- 2. Check and decrement inventory
local remaining = redis.call('DECR', KEYS[3])
if remaining < 0 then
    -- Rollback inventory
    redis.call('INCR', KEYS[3])
    return {0, 'SOLD_OUT'}
end

-- 3. Mark seat as HOLD
redis.call('HSET', KEYS[1], ARGV[1], 'HOLD')

-- 4. Set hold key with TTL (for automatic release)
redis.call('SETEX', KEYS[2], ARGV[3], ARGV[2])

-- 5. Return success with remaining inventory
return {1, tostring(remaining)}
