-- Releases only leases that were acquired by the request.
-- KEYS[1]: user key (empty when not acquired)
-- KEYS[2]: token key (empty when not acquired)
-- ARGV[1]: lease ID

local function release(key)
    if key == '' or redis.call('EXISTS', key) == 0 then
        return
    end
    redis.call('ZREM', key, ARGV[1])
    if redis.call('ZCARD', key) == 0 then
        redis.call('DEL', key)
    end
end

release(KEYS[1])
release(KEYS[2])
return 1
