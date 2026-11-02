# I3 final targeted gateway additions

Owner: sol_contract_review. Root-authorized additions after the broad 142/268 owner regression. No production access, credentials/configuration edits or commit. Current 37-file owner freeze: `I3-owner-final-pre-F1-source-hashes.json`.

Actual final evidence: `I3-T22-T29-account-type-final-windows-unit.jsonl`, Go 1.27.0 Windows, service and handler packages, 6 top-level / 29 including subtests, zero fail/skip. Earlier `I3-T29-repeated-client-request-id-windows-unit.jsonl` and `I3-T22-T29-gateway-final-windows-unit.jsonl` retain the preceding successful test-only slices. This is layered HTTP/unit evidence; environment PG and the whole-I3 F1 are separate and are not inferred here.

## T29

`TestSubscriptionQuotaGatewayRepeatedClientRequestIDCreatesIndependentKnownFacts` sends two actual TCP HTTP requests to a local Gin route which uses actual prepareSubscriptionQuotaForward, actual GatewayService.Forward, mandatory usage scheduling and actual RecordUsage. Both client requests have the same X-Request-ID and Request-ID, and both synthetic provider replies also repeat one provider request ID. Assertions require two different valid server `subquota:UUID` identities, two real upstream sends, one validated known payload per identity, one Apply per identity, and one statistical publication/acknowledgement per identity. Synthetic financial ports assert known precedes Apply and Apply precedes publication; separate PG tests prove actual durable debits.

## T22

`TestCarpoolHTTPCompatibilityI3ResponsesUseFreshCycleSnapshots` invokes the actual OpenAIGatewayHandler.Responses over local TCP HTTP and forwards to an actual local HTTP provider. JSON and SSE each execute two requests. The existing canonical CarpoolGatewayBilling stub intentionally selects a different cycle and request-row identity for each admission. Every known fact, Apply command and usage log must retain its own snapshot, term/cycle and admitted time; both upstream replies expose complete token usage. No unexpected reconciliation occurs. This test covers adapter propagation; it does not claim a real database wall-clock cycle rollover. The environment owner's old-cycle PG recovery test and existing actual WS two-turn cases provide their own layers.

## Account type after lock wait

The only new production-line change in this targeted slice is `ExpectedAccountType: account.Type` at beforeSubscriptionQuotaSend's AdmissionParams construction. Existing actual-send tests now check the exact selected type for native API key, OAuth/setup token, Bedrock API-key/SigV4, Vertex and supported Chat/Responses conversion. `TestSubscriptionQuotaForwardAccountTypeMismatchBeforeSend` exercises Messages, Chat and Responses: the selected sender is API-key while the final admission stub observes OAuth; all return billing-unavailable with no snapshot and zero actual upstream sends. The T29 port also rejects a missing or incorrect expected type. Main owns the final locked database comparison, and environment owns the real account-row lock/type-change rejection test.

Exact final targeted command:

```text
go test -tags=unit -json -count=1 -timeout=90s -run '^Test(SubscriptionQuotaForward(NativeAdmissionAndUsage|ConvertedEndpoints|BedrockAndVertex|AccountTypeMismatchBeforeSend)|SubscriptionQuotaGatewayRepeatedClientRequestIDCreatesIndependentKnownFacts|CarpoolHTTPCompatibilityI3ResponsesUseFreshCycleSnapshots)$' ./internal/service ./internal/handler
```
