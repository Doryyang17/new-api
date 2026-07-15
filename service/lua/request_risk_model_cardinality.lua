-- Tracks exact distinct model cardinality up to the detection threshold.
-- KEYS[1]: model set key
-- ARGV[1]: fixed-length model fingerprint
-- ARGV[2]: maximum stored distinct values
-- ARGV[3]: key TTL in seconds

local key = KEYS[1]
local fingerprint = ARGV[1]
local max_values = tonumber(ARGV[2]) or 8
local ttl = tonumber(ARGV[3]) or 125
local count = redis.call('SCARD', key)

if count < max_values and fingerprint ~= '' then
    redis.call('SADD', key, fingerprint)
    count = redis.call('SCARD', key)
end

redis.call('EXPIRE', key, ttl)
return count
