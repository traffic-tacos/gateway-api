-- enqueue_atomic_streams.lua
-- Atomic enqueue operation: Dedupe check + XADD + TTL setting
--
-- KEYS[1]: dedupe key (e.g., "dedupe:abc123")
-- KEYS[2]: stream key (e.g., "stream:event:{eventID}:user:userID")
--
-- ARGV[1]: token
-- ARGV[2]: event_id
-- ARGV[3]: user_id
-- ARGV[4]: ttl (seconds)
--
-- Returns:
--   {1, streamID} on success (status=1, data=streamID)
--   {0, "DUPLICATE"} on duplicate request (status=0, error_msg)

-- 1. Check for duplicates
if redis.call('EXISTS', KEYS[1]) == 1 then
    return {0, 'DUPLICATE'}
end

-- 2. Get current timestamp (milliseconds, sequence)
local time = redis.call('TIME')
local timestamp_sec = time[1]
local timestamp_usec = time[2]

-- 3. Add to stream
local streamID = redis.call('XADD', KEYS[2], '*',
    'token', ARGV[1],
    'event_id', ARGV[2],
    'user_id', ARGV[3],
    'timestamp', timestamp_sec,
    'timestamp_usec', timestamp_usec
)

-- 4. Set dedupe key with TTL
redis.call('SETEX', KEYS[1], ARGV[4], '1')

-- 5. Return success with stream ID
return {1, streamID}
