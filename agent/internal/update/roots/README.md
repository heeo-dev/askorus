# Root keys of agent updates

The public root keys every agent carries (Phase 1 plan part 27, #35). An
agent takes an update only when a key here vouches for the release key that
signed it.

- One file per key, `root-<n>.pub`, as `askorus-sign root-new` writes it (a
  minisign public key file). Only public keys go here; the secret root key
  stays off the PC, locked with its owner's password.
- A new root key reaches agents only in a release signed under a key they
  already trust. Remove a key only after every agent in use carries another.
- With no key here, agents built from this folder cannot update themselves
  and say so.
