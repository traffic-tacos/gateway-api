-- release_seat_atomic.lua
-- Atomic seat release operation: Remove hold + Increment inventory + Mark as AVAILABLE
--
-- KEYS[1]: seat status hash
-- KEYS[2]: hold key
-- KEYS[3]: inventory counter
--
-- ARGV[1]: seat_id
--
-- Returns:
--   {1, remaining_count} on success (status=1, remaining_inventory)
--   {0, "NOT_HELD"} if seat was not held (status=0, error_msg)

-- 1. Check if seat is held
local status = redis.call('HGET', KEYS[1], ARGV[1])
if status ~= 'HOLD' then
    return {0, 'NOT_HELD'}
end

-- 2. Mark seat as AVAILABLE
redis.call('HSET', KEYS[1], ARGV[1], 'AVAILABLE')

-- 3. Delete hold key
redis.call('DEL', KEYS[2])

-- 4. Increment inventory
local remaining = redis.call('INCR', KEYS[3])

-- 5. Return success with remaining inventory
return {1, tostring(remaining)}
