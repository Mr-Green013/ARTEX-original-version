# third_party/

Vendored, locally-patched third-party modules, wired in via `replace` directives
in the repo-root `go.mod`.

## norma (`github.com/Autumn-27/norma`)

Fork of **norma v0.3.4** (the LLM/agent SDK ARTEX is built on).

**Why forked:** Google's **Gemini 3.x** models require each tool/function call's
opaque `thought_signature` to be sent back verbatim on the assistant turn of the
next request. Via Gemini's OpenAI-compatible endpoint this field travels as
`tool_calls[].extra_content.google.thought_signature`. Upstream norma's generic
OpenAI client dropped it, so multi-turn tool-calling (i.e. every ARTEX agent loop)
failed on Gemini 3 with:

> 400 INVALID_ARGUMENT — "Function call is missing a thought_signature in
> functionCall parts."

This also matters because **newly-created Gemini API keys can no longer use the
Gemini 2.5 models** (they 404 with "no longer available to new users"), leaving
Gemini 3.x — which mandates `thought_signature` — as the only option.

**The patch** (capture on response, replay on request; a no-op for every other
provider because the field is only ever set when a prior Gemini response carried
one):

- `llm/types.go` — add `ContentBlock.ThoughtSignature`.
- `llm/openai.go` — add `extra_content` to `oaToolCall`; capture it in
  `parseOpenAIResponse` and `parseOpenAIFrame` (streaming); replay it in
  `toOpenAIMessages`.
- `llm/provider.go` — carry it on `StreamEvent`.
- `llm/accumulate.go` — record it on the assembled tool-use block.
- `llm/openai_thought_signature_test.go` — round-trip tests.

**Updating:** re-apply this diff on top of a newer upstream norma, or drop the
`replace` directive once upstream ships native `thought_signature` support.
