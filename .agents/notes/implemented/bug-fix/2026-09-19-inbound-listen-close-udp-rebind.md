# Agent Note: inbound Listen Close on bind failure; UDP-only rebind

Status: implemented

## Problem

Named inbound `Listen` (http/socks/mixed/redir/tproxy/tunnel) appended successful listeners then `return err` on a later bind, leaving the already-bound fds open. sudoku continued after errors and returned without closing successes.

`PatchInboundListeners` stored a replacement even when `Listen` failed, so a half-bound (or closed) object became the live map entry.

`ReCreateSocks` / `ReCreateMixed` early-returned when the TCP address matched, even if UDP was nil and now requested. `ReCreateRedir` / `ReCreateTProxy` returned whenever TCP matched, dropping a missing UDP listener.

## Decision

Each of those `Listen` methods `_ = x.Close(); return err` after a later bind fails. `Close` already iterates the success slices. sudoku Closes if any bind failed.

`PatchInboundListeners`: on `Listen` error, Close the replacement and `continue` without storing. Unchanged config still skips (old listener kept). Changed config still Closes the old listener before Listen; a failed replacement is not published.

`ReCreateSocks`/`Mixed`: TCP-same does not skip the UDP bind. `ReCreateRedir`/`TProxy`: TCP-same returns only if UDP is already present; otherwise bind UDP only.

## Alternatives considered

- **Leave leaked fds to process exit** — smallest diff. Rejected: named inbounds and default socks/mixed are recreated on reload; leaked TCP/UDP sockets hold the port.
- **Keep the failed replacement in the map so the next dropOld pass Closes it** — Rejected: REST/status would report a dead listener as live, and a later equal-config skip would never retry Listen.
- **Always Close+rebind TCP when UDP is missing** — simpler flag. Rejected: TCP clients would reset on a UDP-only config change; skip TCP when the address already matches.

## Consequences

- **收益**：failed Listen does not leak bound fds; Patch does not publish a failed replacement; enabling UDP on an existing TCP socks/mixed/redir/tproxy no longer no-ops.
- **代价与已知上限**：Patch still Closes the old listener before the new Listen (same-addr hole, same as [reload-narrow-suspend](../architecture/2026-09-18-reload-narrow-suspend.md)). A failed replacement after that Close leaves no listener until the next Apply. tuic/hysteria2/anytls/sing_tun Listen paths are unchanged.

## Verification

`go test ./listener -run '^$'`. socks/http/mixed/redir/tproxy/tunnel `Listen` Close-before-return on err. `PatchInboundListeners` does not assign after Listen error. Redir TCP-same returns only when `redirUDPListener != nil`.
