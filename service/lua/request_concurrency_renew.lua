-- Renews active user/token concurrency leases without recreating released members.
-- KEYS[1]: user key (empty when not acquired)
-- KEYS[2]: token key (empty when not acquired)
-- ARGV[1]: lease ID
-- ARGV[2]: lease TTL in seconds

local lease_id = ARGV[1]
local ttl = tonumber(ARGV[2]) or 600
local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local expires_at_ms = now_ms + ttl * 1000
local renewed = 0

local function renew(key)
    if key == '' or redis.call('ZSCORE', key, lease_id) == false then
        return
    end
    redis.call('ZADD', key, 'XX', expires_at_ms, lease_id)
    redis.call('EXPIRE', key, ttl + 60)
    renewed = renewed + 1
end

renew(KEYS[1])
renew(KEYS[2])
return renewed
