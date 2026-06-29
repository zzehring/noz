# Security Policy

## Reporting a Vulnerability

Please do not report security vulnerabilities through public GitHub issues,
discussions, or pull requests.

Instead, report them privately via
[GitHub's private vulnerability reporting](https://github.com/zzehring/noz/security/advisories/new).

Please include a description of the issue, steps to reproduce, and the affected
version. You can expect a response within a few weeks confirming whether the
report is accepted, with updates as a fix is worked on. Please keep the report
confidential until a public advisory is published.

## Scope note: the CEL gate is not a sandbox

noz evaluates agent commands against CEL policies (`noz gate`), but it does not
sandbox the agent. The gate is a guardrail, not a security boundary: it is meant
to catch mistakes and enforce conventions, not to contain a hostile process. Do
not rely on it to isolate untrusted code.
