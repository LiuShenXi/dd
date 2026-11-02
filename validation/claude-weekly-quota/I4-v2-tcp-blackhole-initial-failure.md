# TCP blackhole diagnostic history

Before the fixed passing run, the initial test completed its two API return-time
assertions but hung in test teardown awaiting accepted server sockets. A12s test
timeout showed the test goroutine in `accepted.Wait()` and server goroutines in
`io.Copy`, not an API call blocked beyond its deadline. The fixture now explicitly
closes its own accepted sockets before waiting. No service timeout was relaxed.

It also exposed a concrete new-client initialization issue:

```text
failed to initialize maintnotifications: failed to register handler for MOVING:
handler register failed for 'MOVING': cannot overwrite existing handler
```

The new client's shallow Options copy had retained the original client's mutable
push processor. The scope client now creates independent notification state and
rebuilds its default dialer after setting2s timeouts. The project's original
Redis Options builder configures no custom dialer/proxy. TLS configuration is
cloned, and endpoint/network/database/auth options remain unchanged; the original
GPT client and its fault policy are untouched. The passing real TCP test remains
in the final unit regex, rather than substituting a closed-client assertion.
