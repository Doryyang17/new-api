-- Atomically checks and acquires user/token in-flight request leases.
-- KEYS[1]: user key (empty when disabled)
-- KEYS[2]: token key (empty when disabled)
-- ARGV[1]: user limit
-- ARGV[2]: token limit
-- ARGV[3]: lease TTL in seconds
-- ARGV[4]: lease ID
-- ARGV[5]: enforce (1 blocks when exceeded, 0 observes only)

local user_key = KEYS[1]
local token_key = KEYS[2]
local user_limit = tonumber(ARGV[1]) or 0
local token_limit = tonumber(ARGV[2]) or 0
local ttl = tonumber(ARGV[3]) or 600
local lease_id = ARGV[4]
local enforce = ARGV[5] == '1'

local now = redis.call('TIME')
local now_ms = tonumber(now[1]) * 1000 + math.floor(tonumber(now[2]) / 1000)
local expires_at_ms = now_ms + ttl * 1000

local user_count = 0
local token_count = 0
if user_key ~= '' and user_limit > 0 then
    redis.call('ZREMRANGEBYSCORE', user_key, '-inf', now_ms)
    user_count = redis.call('ZCARD', user_key)
end
if token_key ~= '' and token_limit > 0 then
    redis.call('ZREMRANGEBYSCORE', token_key, '-inf', now_ms)
    token_count = redis.call('ZCARD', token_key)
end

local user_exceeded = user_key ~= '' and user_limit > 0 and user_count >= user_limit
local token_exceeded = token_key ~= '' and token_limit > 0 and token_count >= token_limit
if enforce and (user_exceeded or token_exceeded) then
    return {0, user_count, token_count, user_exceeded and 1 or 0, token_exceeded and 1 or 0, 0, 0}
end

local acquired_user = 0
local acquired_token = 0
if user_key ~= '' and user_limit > 0 then
    redis.call('ZADD', user_key, expires_at_ms, lease_id)
    redis.call('EXPIRE', user_key, ttl + 60)
    user_count = user_count + 1
    acquired_user = 1
end
if token_key ~= '' and token_limit > 0 then
    redis.call('ZADD', token_key, expires_at_ms, lease_id)
    redis.call('EXPIRE', token_key, ttl + 60)
    token_count = token_count + 1
    acquired_token = 1
end

return {1, user_count, token_count, user_exceeded and 1 or 0, token_exceeded and 1 or 0, acquired_user, acquired_token}
