# Security Policy

## Reporting a vulnerability

Please report security issues privately via GitHub's **"Report a vulnerability"**
(Security → Advisories) rather than a public issue. We aim to acknowledge within a
few days.

## Threat model (summary)

vaultwright assumes an attacker with **full access to the distributed `vault`
binary** — they can run, disassemble, and patch it. Security therefore rests on
secrets that are *not* in the binary:

- the **password** (a first factor), and
- the **`warden`** binary's private key (the second factor, kept on a trusted machine).

Both are required to derive the asset key; a leaked password alone or a stolen
`vault` alone cannot decrypt. The unlock handshake is fresh each time, so a
captured response cannot be replayed.

**In scope:** hiding asset content/type at rest; resisting a leaked password;
resisting replay.

**Out of scope (by design):** hiding that *encrypted data exists* (entropy analysis
can still see a high-entropy blob); a compromised machine that holds `warden`;
attacks requiring the operator to run `warden` against an attacker's challenge.

`vaultwright seal --no-warden` opts out of the second factor: the vault unlocks on
the password alone, with no `warden` binary produced and no handshake. This is a
real reduction of the threat model above — a leaked password is then sufficient by
itself — so only use it where the warden's threat model genuinely doesn't apply.

## Distribution integrity

Release stubs downloaded on demand are verified against a SHA-256 manifest embedded
in the `vaultwright` binary (the trust root). Do not bypass that verification, and
obtain `vaultwright` itself from a trusted source.
