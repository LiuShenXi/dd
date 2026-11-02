-- Independent user policy. Existing GPT concurrency values remain unchanged.
ALTER TABLE users ADD COLUMN IF NOT EXISTS claude_subscription_concurrency integer;
ALTER TABLE users ADD CONSTRAINT users_claude_subscription_concurrency_positive
    CHECK (claude_subscription_concurrency IS NULL OR claude_subscription_concurrency > 0);
