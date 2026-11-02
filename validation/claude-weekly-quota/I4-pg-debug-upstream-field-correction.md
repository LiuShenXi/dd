The earlier I4 debug HTTP receipts used upstream_called=false to mean that no
external real model provider was called. This name was ambiguous: the isolated
synthetic provider was called by the actual Router/Forward path.

For the successful I4-pg-debug-20261019T172029Z run, the correct explicit fields
are external_upstream_called=false and synthetic_provider_called=true. Its
24Claude HTTP cases and4interleaved GPT calls are verified in the original
JSONL and provider audit. The original receipt/log are retained unchanged.
New runner receipts use these unambiguous fields.
